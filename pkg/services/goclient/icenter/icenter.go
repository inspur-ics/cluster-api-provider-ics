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
	"k8s.io/klog"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/services/goclient/image"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/services/goclient/template"

	infrautilv1 "github.com/inspur-ics/cluster-api-provider-ics/pkg/util"
	basetypv1 "github.com/inspur-ics/ics-go-sdk/client/types"
	basehstv1 "github.com/inspur-ics/ics-go-sdk/host"
	basenetv1 "github.com/inspur-ics/ics-go-sdk/network"
	basestv1 "github.com/inspur-ics/ics-go-sdk/storage"
	basevmv1 "github.com/inspur-ics/ics-go-sdk/vm"
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
	CLOUDINITTYPE string = "OPENSTACK"
)

func CreateVM(ctx *context.VMContext, userdata string) error {
	vmType := ctx.ICSVM.Spec.CloneMode
	if vmType == infrav1.ImportVM {
		return ImportVM(ctx, userdata)
	} else {
		return CloneVM(ctx, userdata)
	}
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
	imageName := ctx.ICSVM.Spec.Template
	ovaImage, err := image.FindOvaImageByName(ctx, imageName)
	if err != nil || ovaImage == nil {
		ctx.Logger.Error(err, "failed to find the ova image from ics")
		return errors.Wrapf(err, "unable to get ova image for %q", ctx)
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

	host, err := getAvailableHosts(ctx, *dataStore, networks)
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
	vmForm.UUID = uuid.New().String()   // the vm path /sys/class/dmi/id/product_uuid
	vmForm.Name = ctx.ICSVM.Name
	vmForm.HostID = host.ID
	vmForm.HostName = host.HostName
	vmForm.HostIP = host.Name
	vmForm.DataStoreID = dataStore.ID
	if vmForm.CPUNum % 2 == 0 {
		vmForm.CPUCore = 2
		vmForm.CPUSocket = vmForm.CPUNum / vmForm.CPUCore
	} else {
		vmForm.CPUCore = 1
		vmForm.CPUSocket = vmForm.CPUNum / vmForm.CPUCore
	}

	diskSpecs, err := getOVADisks(dataStore, ovaConfig.Disks)
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
		DataSourceType: CLOUDINITTYPE,
	}

	virtualMachineService := basevmv1.NewVirtualMachineService(ctx.GetSession().Client)
	task, err := virtualMachineService.ImportVM(ctx, vmForm, ovaFilePath, ovaImage.ServerID, 100)
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
func CloneVM(ctx *context.VMContext, userdata string) error {
	ctx = &context.VMContext{
		ControllerContext: ctx.ControllerContext,
		ICSVM:             ctx.ICSVM,
		Session:           ctx.Session,
		Logger:            ctx.Logger.WithName("icenter"),
		PatchHelper:       ctx.PatchHelper,
	}

	vmTemplate := basetypv1.VirtualMachine{}
	tpl, err := template.FindTemplate(ctx, ctx.ICSVM.Spec.Template)
	if err != nil || tpl == nil {
		ctx.Logger.Error(err, "fail to find the vm template from ics")
		return errors.Wrapf(err, "unable to get vm template for %q", ctx)
	}
	vmTemplate = *tpl
	vmTemplate.UUID = uuid.New().String()   // the vm path /sys/class/dmi/id/product_uuid
	vmTemplate.Name = ctx.ICSVM.Name

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

	host, err := getAvailableHosts(ctx, *dataStore, networks)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the host from ics")
		return errors.Wrapf(err, "unable to get available host for %q", ctx)
	}
	vmTemplate.HostID = host.ID
	vmTemplate.HostName = host.HostName
	vmTemplate.HostIP = host.Name

	diskSpecs, err := getMultiDisks(dataStore, ctx.ICSVM.Spec.Disks, tpl.Disks)
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

	metadata := strings.ReplaceAll(METADATA, "VM_HOST_NAME", vmTemplate.Name)
	metadata = strings.ReplaceAll(metadata, "VM_UUID", vmTemplate.UUID)

	vmTemplate.CloudInit = basetypv1.CloudInit{
		MetaData:       metadata,
		UserData:       userdata,
		DataSourceType: CLOUDINITTYPE,
	}

	if user := ctx.ICSVM.Spec.User; user != nil && user.AuthorizedType == infrav1.PasswordToken {
		vmTemplate.GuestOSAuthInfo.UserName = user.Name
		vmTemplate.GuestOSAuthInfo.UserPwd = user.AuthorizedKey
	}

	klog.Infof("create vm request body: %+v", vmTemplate)

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

func getOVADisks(dataStore *basetypv1.Storage,
	devices []basetypv1.Disk) ([]basetypv1.Disk, error) {

	disks := []basetypv1.Disk{}

	for _, disk := range devices {
		disk.Volume.DataStoreID = dataStore.ID
		disk.Volume.DataStoreName = dataStore.Name
		disk.Volume.Format= "RAW"
		disks = append(disks, disk)
	}

	return disks, nil
}

func getMultiDisks(dataStore *basetypv1.Storage,
	specs []infrav1.DiskSpec, devices []basetypv1.Disk) ([]basetypv1.Disk, error) {

	disks := []basetypv1.Disk{}
	sysDisk := devices[0]
	sysDisk.Volume.DataStoreID = dataStore.ID
	sysDisk.Volume.DataStoreName = dataStore.Name
	sysDisk.Volume.Format= "RAW"
	sysDisk.Volume.Size = float64(specs[0].DiskSize)
	sysDisk.Volume.SizeInByte = int(specs[0].DiskSize) * 1024 * 1024 * 1024
	disks = append(disks, sysDisk)
	if len(specs) >= 2 {
		for i := 1; i < len(specs); i++ {
			disk := initDisk()
			disk.Volume.DataStoreID = dataStore.ID
			disk.Volume.DataStoreName = dataStore.Name
			disk.Volume.Size = float64(specs[i].DiskSize)
			disk.Volume.SizeInByte = int(specs[i].DiskSize) * 1024 * 1024 * 1024
			if specs[i].BusModel != "" {
				disk.BusModel = specs[i].BusModel
			}
			if specs[i].VolumePolicy != "" {
				disk.Volume.VolumePolicy = specs[i].VolumePolicy
			}

			disks = append(disks, disk)
		}
	}

	return disks, nil
}

func initDisk() basetypv1.Disk {
	disk := basetypv1.Disk {
		QueueNum: 1,
		BusModel: "VIRTIO",
		EnableNativeIO: false,
		EnableKernelIO: false,
		TotalIops: 0,
		ReadIops: 0,
		WriteIops: 0,
		TotalBps: 0,
		ReadBps: 0,
		WriteBps: 0,
		Volume: basetypv1.Volume{
			Bootable: false,
			Format: "RAW",
			Shared: false,
			FormatDisk: false,
			VolumePolicy: "THIN",
		},
	}
	return disk
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
			netSpec.VswitchID = network.VswitchDto.ID

			deviceSpec := &ctx.ICSVM.Spec.Network.Devices[index]

			// Check to see if the IP is in the list of the device
			// spec's static IP addresses.
			isStatic := true
			if deviceSpec.DHCP4 || deviceSpec.DHCP6 {
				isStatic = false
				netSpec.Dhcp = true
			}
			if isStatic {
				ip, netmask, err := infrautilv1.GetIPFromNetworkConfig(ctx, deviceSpec)
				if err == nil {
					netSpec.IP = *ip
					netSpec.Netmask = *netmask
					netSpec.Gateway = deviceSpec.Gateway4

					_, err := infrautilv1.CreateOrUpdateIPAddress(ctx, *ip, netSpec)
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

func getAvailableHosts(ctx *context.VMContext,
	dataStore basetypv1.Storage, networks map[int]basetypv1.Network) (basetypv1.Host, error) {
	var (
		host              = basetypv1.Host{}
		storageHostsIndex = map[string]string{}
		networkHostsIndex = map[string]string{}
		availableHosts    []basetypv1.Host
	)

	hostService := basehstv1.NewHostService(ctx.Session.Client)
	hosts, err := hostService.GetHostList(ctx)
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

	for _, host := range hosts {
		if host.ID != "" && host.Status == "CONNECTED" {
			_, storageOK := storageHostsIndex[host.ID]
			_, networkOK := networkHostsIndex[host.ID]
			if storageOK && networkOK {
				availableHosts = append(availableHosts, host)
			}
		}
	}
	if len(availableHosts) > 0 {
		index := rand.Intn(len(availableHosts))
		host = availableHosts[index]
	}
	return host, nil
}
