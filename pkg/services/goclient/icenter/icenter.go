/*
Copyright 2024 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package icenter

import (
	"math/rand"
	"strings"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	basetypv1 "github.com/inspur-ics/ics-go-sdk/client/types"
	basecluv1 "github.com/inspur-ics/ics-go-sdk/cluster"
	basehstv1 "github.com/inspur-ics/ics-go-sdk/host"
	basenetv1 "github.com/inspur-ics/ics-go-sdk/network"
	basestv1 "github.com/inspur-ics/ics-go-sdk/storage"
	basevmv1 "github.com/inspur-ics/ics-go-sdk/vm"

	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/services/goclient/image"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/services/goclient/template"
	infrautilv1 "github.com/inspur-ics/cluster-api-provider-ics/pkg/util"
)

const (
	// cloud-init metadata config
	METADATA string = `
{
    "hostname": "VM_HOST_NAME",
    "launch_index": 0,
    "name":"VM_HOST_NAME",
    "uuid":"VM_UUID"
}
`
)

func CreateVM(ctx *context.VMContext, userdata string) error {
	return ImportVM(ctx, userdata)
}

// nolint:gocognit
func ImportVM(ctx *context.VMContext, userdata string) error {
	ctx = &context.VMContext{
		ControllerContext: ctx.ControllerContext,
		ICSVM:             ctx.ICSVM,
		Session:           ctx.Session,
		Logger:            ctx.Logger.WithName("icenter"),
		PatchHelper:       ctx.PatchHelper,
	}
	imageName := ctx.ICSVM.Spec.Template + ".ova"
	ovaImage, err := image.FindOvaImageByName(ctx, imageName)
	if err != nil {
		ctx.Logger.Error(err, "failed to find the ova image from ics")
		return errors.Wrapf(err, "unable to get ova image for %q", ctx)
	}

	clusterService := basecluv1.NewClusterService(ctx.Session.Client)
	cluster, err := clusterService.GetClusterByName(ctx, ctx.ICSVM.Spec.Cluster)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the cluster from ics")
		return errors.Wrapf(err, "unable to get cluster for %q", ctx)
	}

	storageService := basestv1.NewStorageService(ctx.GetSession().Client)
	dataStore, err := storageService.GetStorageInfoByName(ctx, ctx.ICSVM.Spec.Datastore)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the data store from ics")
		return errors.Wrapf(err, "unable to get DataStore for %q", ctx)
	}

	networks := make(map[int]basetypv1.Network)
	networkService := basenetv1.NewNetworkService(ctx.GetSession().Client)
	for index, device := range ctx.ICSVM.Spec.Network.Devices {
		network, err := networkService.GetNetworkByName(ctx, device.NetworkName)
		if err != nil {
			ctx.Logger.Error(err, "fail to find the network devices from ics")
			return errors.Wrapf(err, "unable to get networks for %q", ctx)
		}
		networks[index] = *network
	}

	host, err := getAvailableHosts(ctx, *cluster, *dataStore, networks)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the host from ics")
		return errors.Wrapf(err, "unable to get available host for %q", ctx)
	}

	ovaFilePath := ovaImage.Path + "/" + ovaImage.Name
	ovaConfig, err := image.GetVMForm(ctx, ovaFilePath, host.ID, ovaImage.ServerID)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the vm template from ics")
		return errors.Wrapf(err, "unable to get vm template for %q", ctx)
	}
	vmForm := *ovaConfig
	vmForm.UUID = uuid.New().String()
	vmForm.Name = ctx.ICSVM.Name
	vmForm.HostID = host.ID
	vmForm.HostName = host.HostName
	vmForm.HostIP = host.Name

	diskSpecs, err := getDiskSpecs(dataStore, ovaConfig.Disks)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the disk spec")
		return errors.Wrapf(err, "error getting disk spec for %q", ctx)
	}
	vmForm.Disks = diskSpecs

	networkSpecs, err := getNetworkSpecs(ctx, ovaConfig.Nics, networks)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the network spec")
		return errors.Wrapf(err, "error getting network specs for %q", ctx)
	}
	vmForm.Nics = networkSpecs

	metadata := strings.ReplaceAll(METADATA, "VM_HOST_NAME", vmForm.Name)
	metadata = strings.ReplaceAll(metadata, "VM_UUID", vmForm.UUID)

	vmForm.CloudInit = basetypv1.CloudInit{
		MetaData:       metadata,
		UserData:       userdata,
		DataSourceType: "OPENSTACK",
	}

	virtualMachineService := basevmv1.NewVirtualMachineService(ctx.GetSession().Client)
	task, err := virtualMachineService.ImportVM(ctx, vmForm, ovaFilePath, host.ID)
	if err != nil {
		ctx.Logger.Error(err, "failed to import vm by the ova image")
		return errors.Wrapf(err, "error import vm for machine %s", ctx)
	}

	ctx.ICSVM.Status.TaskRef = task.TaskId

	// patch the icsVM early to ensure that the task is
	// reflected in the status right away, this avoid situations
	// of concurrent clones
	if err := ctx.Patch(); err != nil {
		ctx.Logger.Error(err, "patch failed", "icsvm", ctx.ICSVM)
	}

	return nil
}

// Clone kicks off a clone operation on vCenter to create a new virtual machine.
// nolint:gocognit
func CloneVM(ctx *context.VMContext, metadata []byte) error {
	ctx = &context.VMContext{
		ControllerContext: ctx.ControllerContext,
		ICSVM:             ctx.ICSVM,
		Session:           ctx.Session,
		Logger:            ctx.Logger.WithName("icenter"),
		PatchHelper:       ctx.PatchHelper,
	}

	vmTemplate := basetypv1.VirtualMachine{}
	tpl, err := template.FindTemplate(ctx, ctx.ICSVM.Spec.Template)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the vm template from ics")
		return errors.Wrapf(err, "unable to get vm template for %q", ctx)
	}
	vmTemplate = *tpl
	vmTemplate.Name = ctx.ICSVM.Name

	clusterService := basecluv1.NewClusterService(ctx.Session.Client)
	cluster, err := clusterService.GetClusterByName(ctx, ctx.ICSVM.Spec.Cluster)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the cluster from ics")
		return errors.Wrapf(err, "unable to get cluster for %q", ctx)
	}

	storageService := basestv1.NewStorageService(ctx.GetSession().Client)
	dataStore, err := storageService.GetStorageInfoByName(ctx, ctx.ICSVM.Spec.Datastore)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the data store from ics")
		return errors.Wrapf(err, "unable to get DataStore for %q", ctx)
	}

	networks := make(map[int]basetypv1.Network)
	networkService := basenetv1.NewNetworkService(ctx.GetSession().Client)
	for index, device := range ctx.ICSVM.Spec.Network.Devices {
		network, err := networkService.GetNetworkByName(ctx, device.NetworkName)
		if err != nil {
			ctx.Logger.Error(err, "fail to find the network devices from ics")
			return errors.Wrapf(err, "unable to get networks for %q", ctx)
		}
		networks[index] = *network
	}

	host, err := getAvailableHosts(ctx, *cluster, *dataStore, networks)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the host from ics")
		return errors.Wrapf(err, "unable to get available host for %q", ctx)
	}
	vmTemplate.HostID = host.ID
	vmTemplate.HostName = host.HostName
	vmTemplate.HostIP = host.Name

	diskSpecs, err := getDiskSpecs(dataStore, tpl.Disks)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the disk spec")
		return errors.Wrapf(err, "error getting disk spec for %q", ctx)
	}
	vmTemplate.Disks = diskSpecs

	networkSpecs, err := getNetworkSpecs(ctx, tpl.Nics, networks)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the network spec")
		return errors.Wrapf(err, "error getting network specs for %q", ctx)
	}
	vmTemplate.Nics = networkSpecs

	virtualMachineService := basevmv1.NewVirtualMachineService(ctx.GetSession().Client)
	task, err := virtualMachineService.CreateVMByTemplate(ctx, vmTemplate, true)
	if err != nil {
		ctx.Logger.Error(err, "fail to create vm by the template")
		return errors.Wrapf(err, "error trigging clone op for machine %s", ctx)
	}

	ctx.ICSVM.Status.TaskRef = task.TaskId

	// patch the icsVM early to ensure that the task is
	// reflected in the status right away, this avoid situations
	// of concurrent clones
	if err := ctx.Patch(); err != nil {
		ctx.Logger.Error(err, "patch failed", "icsvm", ctx.ICSVM)
	}
	return nil
}

func getDiskSpecs(dataStore *basetypv1.Storage,
	devices []basetypv1.Disk) ([]basetypv1.Disk, error) {

	disks := []basetypv1.Disk{}

	for _, disk := range devices {
		disk.Volume.DataStoreID = dataStore.ID
		disk.Volume.DataStoreName = dataStore.Name
		disks = append(disks, disk)
	}

	return disks, nil
}

func getNetworkSpecs(ctx *context.VMContext, devices []basetypv1.Nic,
	networks map[int]basetypv1.Network) ([]basetypv1.Nic, error) {

	deviceSpecs := []basetypv1.Nic{}

	// Add new NICs based on the machine config.
	for index, nic := range devices {
		netSpec := nic
		if len(ctx.ICSVM.Spec.Network.Devices) > index {
			network := networks[index]
			netSpec.DeviceID = network.ID
			netSpec.DeviceName = network.Name
			netSpec.NetworkID = network.ID

			deviceSpec := &ctx.ICSVM.Spec.Network.Devices[index]

			// Check to see if the IP is in the list of the device
			// spec's static IP addresses.
			isStatic := true
			if deviceSpec.DHCP4 || deviceSpec.DHCP6 {
				isStatic = false
			}
			if isStatic {
				ip, netmask, err := infrautilv1.GetIPFromNetworkConfig(ctx, deviceSpec)
				if err == nil {
					netSpec.IP = *ip
					netSpec.Netmask = *netmask
					netSpec.Gateway = deviceSpec.Gateway4

					_, err := infrautilv1.ReconcileIPAddress(ctx, *ip, netSpec)
					if err != nil {
						continue
					}
				}
			}
		}

		deviceSpecs = append(deviceSpecs, netSpec)
	}

	return deviceSpecs, nil
}

func getAvailableHosts(ctx *context.VMContext, cluster basetypv1.Cluster,
	dataStore basetypv1.Storage, networks map[int]basetypv1.Network) (basetypv1.Host, error) {
	var (
		host              = basetypv1.Host{}
		storageHostsIndex = map[string]string{}
		networkHostsIndex = map[string]string{}
		availableHosts    []basetypv1.Host
	)

	hostService := basehstv1.NewHostService(ctx.Session.Client)
	clusterHosts, err := hostService.GetHostListByClusterID(ctx, cluster.Id)
	if err != nil {
		return basetypv1.Host{}, err
	}

	storageHosts, err := hostService.GetHostListByStorageID(ctx, dataStore.ID)
	if err != nil {
		return basetypv1.Host{}, err
	}
	for _, host := range storageHosts {
		if host.ID != "" {
			storageHostsIndex[host.ID] = host.Name
		}
	}

	hostBounds := make(map[string]int)
	for _, network := range networks {
		networkHosts, err := hostService.GetHostListByNetworkID(ctx, network.ID)
		if err != nil {
			return basetypv1.Host{}, err
		}
		for _, host := range networkHosts {
			if host.ID != "" {
				hostBounds[host.ID]++
			}
		}
	}
	totalBounds := len(networks)
	for hostID, bounds := range hostBounds {
		if bounds == totalBounds {
			networkHostsIndex[hostID] = "network"
		}
	}

	for _, host := range clusterHosts {
		if host.ID != "" && host.Status == "CONNECTED" {
			_, storageOK := storageHostsIndex[host.ID]
			_, networkOK := networkHostsIndex[host.ID]
			if storageOK && networkOK {
				availableHosts = append(availableHosts, *host)
			}
		}
	}
	if len(availableHosts) > 0 {
		index := rand.Intn(len(availableHosts))
		host = availableHosts[index]
	}
	return host, nil
}
