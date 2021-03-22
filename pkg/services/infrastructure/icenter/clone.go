/*
Copyright 2019 The Kubernetes Authors.

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
	"github.com/inspur-ics/ics-go-sdk/client/types"
	clusterapi "github.com/inspur-ics/ics-go-sdk/cluster"
	hostapi "github.com/inspur-ics/ics-go-sdk/host"
	networkapi "github.com/inspur-ics/ics-go-sdk/network"
	storageapi "github.com/inspur-ics/ics-go-sdk/storage"
	vmapi "github.com/inspur-ics/ics-go-sdk/vm"
	"github.com/pkg/errors"
	"math/rand"

	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/services/infrastructure/template"
	infrautilv1 "github.com/inspur-ics/cluster-api-provider-ics/pkg/util"
)

// Clone kicks off a clone operation on vCenter to create a new virtual machine.
// nolint:gocognit
func Clone(ctx *context.VMContext) error {
	ctx = &context.VMContext{
		ControllerContext: ctx.ControllerContext,
		ICSVM:             ctx.ICSVM,
		Session:           ctx.Session,
		Logger:            ctx.Logger.WithName("icenter"),
		PatchHelper:       ctx.PatchHelper,
	}
	ctx.Logger.Info("starting clone process")

	vmTemplate := types.VirtualMachine{}
	tpl, err := template.FindTemplate(ctx, ctx.ICSVM.Spec.Template)
	if err != nil {
		return errors.Wrapf(err, "unable to get vm template for %q", ctx)
	}
	vmTemplate.ID = tpl.ID
	vmTemplate.Name = ctx.ICSVM.Name

	clusterService := clusterapi.NewClusterService(ctx.Session.Client)
	cluster, err := clusterService.GetClusterByName(ctx, ctx.ICSVM.Spec.Cluster)
	if err != nil {
		return errors.Wrapf(err, "unable to get cluster for %q", ctx)
	}

	//TODO [WYC]ics system no resource pool design
	//pool, err := ctx.Session.Finder.ResourcePoolOrDefault(ctx, ctx.ICSVM.Spec.ResourcePool)
	//if err != nil {
	//	return errors.Wrapf(err, "unable to get resource pool for %q", ctx)
	//}

	storageService := storageapi.NewStorageService(ctx.GetSession().Client)
	dataStore, err := storageService.GetStorageInfoByName(ctx, ctx.ICSVM.Spec.Datastore)
	if err != nil {
		return errors.Wrapf(err, "unable to get DataStore for %q", ctx)
	}

	networks := make(map[int]types.Network)
	networkService := networkapi.NewNetworkService(ctx.GetSession().Client)
	for index, device := range ctx.ICSVM.Spec.Network.Devices {
		network, err := networkService.GetNetworkByName(ctx, device.NetworkName)
		if err != nil {
			return errors.Wrapf(err, "unable to get cluster for %q", ctx)
		}
		networks[index] = *network
	}

	host, err := getAvailableHosts(ctx, *cluster, *dataStore, networks)
	if err != nil {
		return errors.Wrapf(err, "unable to get available host for %q", ctx)
	}
	vmTemplate.HostID = host.ID
	vmTemplate.HostName = host.Name

	diskSpecs, err := getDiskSpecs(dataStore, tpl.Disks)
	if err != nil {
		return errors.Wrapf(err, "error getting disk spec for %q", ctx)
	}
	vmTemplate.Disks = diskSpecs

	networkSpecs, err := getNetworkSpecs(ctx, tpl.Nics, networks)
	if err != nil {
		return errors.Wrapf(err, "error getting network specs for %q", ctx)
	}
	vmTemplate.Nics = networkSpecs

	virtualMachineService := vmapi.NewVirtualMachineService(ctx.GetSession().Client)
	task, err := virtualMachineService.CreateVMByTemplate(ctx, vmTemplate, true)
	if err != nil {
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

func getDiskSpecs(dataStore *types.Storage,
	devices []types.Disk) ([]types.Disk, error) {

	var disks []types.Disk

	for _, disk := range devices {
		diskSpec := types.Disk{
			Volume:types.Volume{
				ID: disk.Volume.ID,
				DataStoreID: dataStore.ID,
				DataStoreName: dataStore.Name,
			},
		}
		disks = append(disks, diskSpec)
	}

	return disks, nil
}

func getNetworkSpecs(
	ctx *context.VMContext,
	devices []types.Nic,
	networks map[int]types.Network) ([]types.Nic, error) {

	var deviceSpecs []types.Nic

	// Add new NICs based on the machine config.
	for index, nic := range devices {
		netSpec := nic
		if len(ctx.ICSVM.Spec.Network.Devices) > index {
			network := networks[index]
			netSpec = types.Nic{
				DeviceID:   network.ID,
				DeviceName: network.Name,
				NetworkID:  network.ID,
			}

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
					netSpec.IP      = *ip
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

func getAvailableHosts(ctx *context.VMContext, cluster types.Cluster,
	dataStore types.Storage, networks map[int]types.Network) (types.Host, error) {
	var (
		host              = types.Host{}
		storageHostsIndex = map[string]string{}
		networkHostsIndex = map[string]string{}
		availableHosts      []types.Host
	)

	hostService := hostapi.NewHostService(ctx.Session.Client)
	clusterHosts, err := hostService.GetHostListByClusterID(ctx, cluster.Id)
	if err != nil {
		return types.Host{}, err
	}

	storageHosts, err := hostService.GetHostListByStorageID(ctx, dataStore.ID)
	if err != nil {
		return types.Host{}, err
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
			return types.Host{}, err
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
