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
	"github.com/inspur-ics/ics-go-sdk/storage"
	vmapi "github.com/inspur-ics/ics-go-sdk/vm"
	"github.com/pkg/errors"

	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/services/infrastructure/template"
)

// Clone kicks off a clone operation on vCenter to create a new virtual machine.
// nolint:gocognit
func Clone(ctx *context.VMContext, bootstrapData []byte) error {
	ctx = &context.VMContext{
		ControllerContext: ctx.ControllerContext,
		ICSVM:         ctx.ICSVM,
		Session:           ctx.Session,
		Logger:            ctx.Logger.WithName("icenter"),
		PatchHelper:       ctx.PatchHelper,
	}
	ctx.Logger.Info("starting clone process")

	//TODO [WYC] Completed clone vm and edit vm add extraConfig
	//var extraConfig extra.Config
	//if len(bootstrapData) > 0 {
	//	ctx.Logger.Info("applied bootstrap data to VM clone spec")
	//	if err := extraConfig.SetCloudInitUserData(bootstrapData); err != nil {
	//		return err
	//	}
	//}

	tpl, err := template.FindTemplate(ctx, ctx.ICSVM.Spec.Template)
	if err != nil {
		return err
	}

	//datastore, err := ctx.Session.Finder.DatastoreOrDefault(ctx, ctx.ICSVM.Spec.Datastore)
	//if err != nil {
	//	return errors.Wrapf(err, "unable to get datastore for %q", ctx)
	//}

	//TODO [WYC]ics system no resource pool design
	//pool, err := ctx.Session.Finder.ResourcePoolOrDefault(ctx, ctx.ICSVM.Spec.ResourcePool)
	//if err != nil {
	//	return errors.Wrapf(err, "unable to get resource pool for %q", ctx)
	//}

	storageService := storage.NewStorageService(ctx.GetSession().Client)
	dataStore, err := storageService.GetDatastoreByName(ctx, ctx.ICSVM.Spec.Datastore)
	if err != nil {
		return errors.Wrapf(err, "unable to get DataStore for %q", ctx)
	}

	diskSpecs, err := getDiskSpecs(dataStore, tpl.Disks)
	if err != nil {
		return errors.Wrapf(err, "error getting disk spec for %q", ctx)
	}
	tpl.Disks = diskSpecs

	networkSpecs, err := getNetworkSpecs(ctx, tpl.Nics)
	if err != nil {
		return errors.Wrapf(err, "error getting network specs for %q", ctx)
	}
	tpl.Nics = networkSpecs

	virtualMachineService := vmapi.NewVirtualMachineService(ctx.GetSession().Client)
	task, err := virtualMachineService.CreateVMByTemplate(ctx, *tpl, true)
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
	devices []types.Nic) ([]types.Nic, error) {

	var deviceSpecs []types.Nic

	// Add new NICs based on the machine config.
	for index, nic := range devices {
		var netSpec types.Nic
		if len(ctx.ICSVM.Spec.Network.Devices) > index {
			spec := &ctx.ICSVM.Spec.Network.Devices[index]
			//TODO [WYC API WAITING] Query Network Information
			//ref, err := ctx.Session.Finder.Network(ctx, netSpec.NetworkName)
			//if err != nil {
			//	return nil, errors.Wrapf(err, "unable to find network %q", netSpec.NetworkName)
			//}
			netSpec = types.Nic{
				DeviceID:   nic.DeviceID,
				DeviceName: nic.DeviceName,
				NetworkID:  nic.NetworkID,
			}
			//TODO [WYC API WAITING] static network config
			netSpec.Gateway = spec.Gateway4
		} else {
			netSpec = types.Nic{
				DeviceID:   nic.DeviceID,
				DeviceName: nic.DeviceName,
				NetworkID:  nic.NetworkID,
			}
			if len(nic.IP) != 0 {
				netSpec.IP = nic.IP
				netSpec.Netmask = nic.Netmask
				netSpec.Gateway = nic.Gateway
			}
		}

		deviceSpecs = append(deviceSpecs, netSpec)
	}

	return deviceSpecs, nil
}
