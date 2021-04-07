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

package infrastructure

import (
	"encoding/base64"
	"github.com/pkg/errors"
	"time"

	corev1 "k8s.io/api/core/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1alpha3"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/services/infrastructure/net"
	infrautilv1 "github.com/inspur-ics/cluster-api-provider-ics/pkg/util"
	taskapi "github.com/inspur-ics/ics-go-sdk/task"
	vmapi "github.com/inspur-ics/ics-go-sdk/vm"
)

// VMService provdes API to interact with the VMs using govmomi
type VMService struct{}

// ReconcileVM makes sure that the VM is in the desired state by:
//   1. Creating the VM if it does not exist, then...
//   2. Updating the VM with the bootstrap data, such as the cloud-init meta and user data, before...
//   3. Powering on the VM, and finally...
//   4. Returning the real-time state of the VM to the caller
func (vms *VMService) ReconcileVM(ctx *context.VMContext) (vm infrav1.VirtualMachine, _ error) {
	// Initialize the result.
	vm = infrav1.VirtualMachine{
		Name:  ctx.ICSVM.Name,
		State: infrav1.VirtualMachineStatePending,
	}

	// If there is an in-flight task associated with this VM then do not
	// reconcile the VM until the task is completed.
	if inFlight, err := reconcileInFlightTask(ctx); err != nil || inFlight {
		return vm, err
	}

	// This deferred function will trigger a reconcile event for the
	// ICSVM resource once its associated task completes. If
	// there is no task for the ICSVM resource then no reconcile
	// event is triggered.
	defer reconcileICSVMOnTaskCompletion(ctx)

	// Before going further, we need the VM's managed object reference.
	vmRef, err := findVM(ctx)
	if err != nil {
		ctx.Logger.Error(err,"fail to get vm object reference")

		// Otherwise, this is a new machine and the  the VM should be created.
		// Create the VM.
		return vm, createVM(ctx)
	}

	//
	// At this point we know the VM exists, so it needs to be updated.
	//

	// Create a new virtualMachineContext to reconcile the VM.
	vmCtx := &virtualMachineContext{
		VMContext: *ctx,
		Obj:       vmapi.NewVirtualMachineService(ctx.Session.Client),
		Ref:       vmRef,
		State:     &vm,
	}

	vms.reconcileUUID(vmCtx)

	//TODO [WYC] check network business
	if err := vms.reconcileNetworkStatus(vmCtx); err != nil {
		return vm, err
	}

	// ICS VM exec cloud init configuration data
	if ok, err := vms.reconcileCloudInit(vmCtx); err != nil || !ok {
		return vm, err
	}

	// Get the bootstrap data.
	bootstrapData, err := vms.getBootstrapData(ctx)
	if err != nil {
		return vm, err
	}

	// ICS VM check and update cloud init configuration data
	if ok, err := vms.reconcileMetadata(vmCtx, bootstrapData); err != nil || !ok {
		return vm, err
	}

	if ok, err := vms.reconcilePowerState(vmCtx); err != nil || !ok {
		return vm, err
	}

	ctx.Logger.Info("ReconcileVM end")
	vm.State = infrav1.VirtualMachineStateReady
	return vm, nil
}

// DestroyVM powers off and destroys a virtual machine.
func (vms *VMService) DestroyVM(ctx *context.VMContext) (infrav1.VirtualMachine, error) {

	vm := infrav1.VirtualMachine{
		Name:  ctx.ICSVM.Name,
		State: infrav1.VirtualMachineStatePending,
	}

	// If there is an in-flight task associated with this VM then do not
	// reconcile the VM until the task is completed.
	if inFlight, err := reconcileInFlightTask(ctx); err != nil || inFlight {
		return vm, err
	}

	// This deferred function will trigger a reconcile event for the
	// ICSVM resource once its associated task completes. If
	// there is no task for the ICSVM resource then no reconcile
	// event is triggered.
	defer reconcileICSVMOnTaskCompletion(ctx)

	// Before going further, we need the VM's managed object reference.
	vmRef, err := findVM(ctx)
	if err != nil {
		ctx.Logger.Error(err, "fail to get vm object reference")
		// If the VM's MoRef could not be found then the VM no longer exists. This
		// is the desired state.
		if isNotFound(err) {
			vm.State = infrav1.VirtualMachineStateNotFound
			return vm, nil
		}
		return vm, err
	}

	//
	// At this point we know the VM exists, so it needs to be destroyed.
	//

	// Create a new virtualMachineContext to reconcile the VM.
	vmCtx := &virtualMachineContext{
		VMContext: *ctx,
		Obj:       vmapi.NewVirtualMachineService(ctx.Session.Client),
		Ref:       vmRef,
		State:     &vm,
	}

	// Power off the VM.
	powerState, err := vms.getPowerState(vmCtx)
	if err != nil {
		ctx.Logger.Error(err, "fail to get power state of the vm")
		return vm, err
	}
	if powerState == infrav1.VirtualMachinePowerStatePoweredOn {
		task, err := vmCtx.Obj.PowerOffVM(ctx, vmRef.Value)
		if err != nil {
			ctx.Logger.Error(err, "power off the vm error")
			return vm, err
		}
		ctx.ICSVM.Status.TaskRef = task.TaskId
		ctx.Logger.Info("wait for VM to be powered off")
		return vm, nil
	}

	// Clear IPAddresses
	//err = vms.reconcileDeleteIPAddress(ctx)
	//if err != nil {
	//	return vm, nil
	//}

	// At this point the VM is not powered on and can be destroyed. Store the
	// destroy task's reference and return a requeue error.
	ctx.Logger.Info("destroying vm", "vmRef", vmRef)
	task, err := vmCtx.Obj.DeleteVM(ctx, vmRef.Value, true, true)
	if err != nil {
		ctx.Logger.Error(err, "fail to destroying vm")
		return vm, err
	}
	ctx.ICSVM.Status.TaskRef = task.TaskId
	ctx.Logger.Info("wait for VM to be destroyed")
	return vm, nil
}

func (vms *VMService) reconcileNetworkStatus(ctx *virtualMachineContext) error {
	ctx.Logger.Info("reconciling reconcileNetworkStatus staring...")
	netStatus, err := vms.getNetworkStatus(ctx)
	if err != nil {
		ctx.Logger.Info("reconciling reconcileNetworkStatus ended", "err", err)
		return err
	}
	ctx.State.Network = netStatus
	if len(netStatus) >= 1 {
		if ctx.ICSVM.Status.Addresses == nil && netStatus[0].IPAddrs != nil {
			infrautilv1.UpdateNetworkInfo(&ctx.VMContext, netStatus)
			if infrautilv1.IsControlPlaneMachine(ctx.ICSVM) {
				err = ctx.Patch()
				if err != nil {
					ctx.Logger.Error(err, "ICSVM Path IPAddress Error")
				}
				ctx.Logger.Info("ICSVM Path IPAddress", "netStatus", netStatus)
			}
		}
	}
	ctx.Logger.Info("reconciling reconcileNetworkStatus ended", "netStatus", netStatus)
	return nil
}

func (vms *VMService) reconcileMetadata(ctx *virtualMachineContext, newMetadata []byte) (bool, error) {
	existingMetadata, err := vms.getMetadata(ctx)
	if err != nil {
		return false, err
	}

	// If the metadata is the same then return early.
	if string(newMetadata) == existingMetadata {
		return true, nil
	}

	ctx.Logger.Info("updating metadata")
	taskRef, err := vms.setMetadata(ctx, newMetadata)
	if err != nil {
		return false, errors.Wrapf(err, "unable to set metadata on vm %s", ctx)
	}
	time.Sleep(time.Duration(12) * time.Second)

	ctx.ICSVM.Status.TaskRef = taskRef
	ctx.Logger.Info("VM metadata to be updated")
	return false, nil
}

func (vms *VMService) reconcilePowerState(ctx *virtualMachineContext) (bool, error) {
	powerState, err := vms.getPowerState(ctx)
	if err != nil {
		return false, err
	}
	switch powerState {
	case infrav1.VirtualMachinePowerStatePoweredOff:
		ctx.Logger.Info("powering on")
		task, err := ctx.Obj.PowerOnVM(ctx, ctx.Ref.Value)
		if err != nil {
			return false, errors.Wrapf(err, "failed to trigger power on op for vm %s", ctx)
		}

		// Update the ICSVM.Status.TaskRef to track the power-on task.
		ctx.ICSVM.Status.TaskRef = task.TaskId

		// Once the VM is successfully powered on, a reconcile request should be
		// triggered once the VM reports IP addresses are available.
		reconcileICSVMWhenNetworkIsReady(ctx, task)

		ctx.Logger.Info("wait for VM to be powered on")
		return false, nil
	case infrav1.VirtualMachinePowerStatePoweredOn:
		ctx.Logger.Info("powered on")
		return true, nil
	default:
		return false, errors.Errorf("unexpected power state %q for vm %s", powerState, ctx)
	}
}

func (vms *VMService) reconcileCloudInit(ctx *virtualMachineContext) (bool, error) {
	vmObj, err := ctx.Obj.GetVM(ctx, ctx.Ref.Value)
	if err != nil {
		return false, errors.Errorf("get vm %s info err", ctx.Ref.Value)
	}

	if vmObj != nil && len(vmObj.ExtendData) > 0 {
		return true, nil
	}

	ctx.Logger.Info("restarting vm on")

	if vmObj.Status == "STOPPED" {
		ctx.Logger.Info("first powering on")
		powerOnTask, err := ctx.Obj.PowerOnVM(ctx, ctx.Ref.Value)
		if err != nil {
			return false, errors.Wrapf(err, "failed to trigger power on op for vm %s", ctx)
		}

		// Wait for the VM to be powered off.
		taskService := taskapi.NewTaskService(ctx.Session.Client)
		powerOnTaskInfo, err := taskService.WaitForResult(ctx, powerOnTask)
		if err != nil && powerOnTaskInfo == nil {
			ctx.Logger.Error(err, "ics task tracing error.", "id", powerOnTask.TaskId)
		}
		time.Sleep(time.Duration(24) * time.Second)

		powerOffTask, err := ctx.Obj.PowerOffVM(ctx, ctx.Ref.Value)
		if err != nil {
			return false, errors.Wrapf(err, "failed to trigger power off op for vm %s", ctx)
		}

		// Wait for the VM to be powered off.
		taskService = taskapi.NewTaskService(ctx.Session.Client)
		powerOffTaskInfo, err := taskService.WaitForResult(ctx, powerOffTask)
		if err != nil && powerOffTaskInfo == nil {
			ctx.Logger.Error(err, "ics task tracing error.", "id", powerOffTask.TaskId)
		}
		time.Sleep(time.Duration(12) * time.Second)
	}

	ctx.Logger.Info("Reconcile CloudInit Will Starting ...")
	return true, nil
}

func (vms *VMService) reconcileUUID(ctx *virtualMachineContext) {
	vm, err := ctx.Obj.GetVM(ctx, ctx.Ref.Value)
	if err != nil {
		return
	}
	ctx.State.UID = vm.ID
	ctx.State.BiosUUID = vm.UUID
}

func (vms *VMService) getPowerState(ctx *virtualMachineContext) (infrav1.VirtualMachinePowerState, error) {
	vmObj, err := ctx.Obj.GetVM(ctx, ctx.Ref.Value)
	if err != nil {
		ctx.Logger.Error(err,"fail to get vm info from ics")
		return "", err
	}

	switch vmObj.Status {
	case "STARTED":
		return infrav1.VirtualMachinePowerStatePoweredOn, nil
	case "STOPPED":
		return infrav1.VirtualMachinePowerStatePoweredOff, nil
	case "PAUSED":
		return infrav1.VirtualMachinePowerStateSuspended, nil
	case "RESTARTING":
		return infrav1.VirtualMachinePowerStateSuspended, nil
	case "PENDING":
		return infrav1.VirtualMachinePowerStateSuspended, nil
	default:
		return "", errors.Errorf("unexpected power state %q for vm %s", vmObj.Status, ctx)
	}
}

func (vms *VMService) getMetadata(ctx *virtualMachineContext) (string, error) {
	vm, err := ctx.Obj.GetVM(ctx, ctx.Ref.Value)
	if err != nil {
		return "", errors.Wrapf(err, "unable to cloud init meta data for vm %s", ctx.Ref.Value)
	}

	metadataBase64 := vm.ExtendData
	if metadataBase64 == "" {
		return "", nil
	}

	metadataBuf, err := base64.StdEncoding.DecodeString(metadataBase64)
	if err != nil {
		return "", errors.Wrapf(err, "unable to decode metadata for %s", ctx)
	}

	return string(metadataBuf), nil
}

func (vms *VMService) setMetadata(ctx *virtualMachineContext, metadata []byte) (string, error) {
	metadataBase64 := base64.StdEncoding.EncodeToString(metadata)

	vmObj, err := ctx.Obj.GetVM(ctx, ctx.Ref.Value)
	if err != nil {
		return "", errors.Wrapf(err, "unable to get vm %s", ctx.Ref.Value)
	}

	vmObj.ExtendData = metadataBase64
	vmObj.CloudInited = true

	task, err := ctx.Obj.SetVM(ctx, *vmObj)
	if err != nil {
		return "", errors.Wrapf(err, "unable to set metadata on vm %s", ctx)
	}

	// Wait for the VM to be edited.
	taskService := taskapi.NewTaskService(ctx.Session.Client)
	_, _ = taskService.WaitForResult(ctx, task)

	return task.TaskId, nil
}

func (vms *VMService) getNetworkStatus(ctx *virtualMachineContext) ([]infrav1.NetworkStatus, error) {
	allNetStatus, err := net.GetNetworkStatus(&ctx.VMContext, ctx.Session.Client, ctx.Ref)
	if err != nil {
		ctx.Logger.Info("got allNetStatus", "err", err)
		return nil, err
	}
	ctx.Logger.Info("got allNetStatus", "status", allNetStatus)
	var apiNetStatus []infrav1.NetworkStatus
	for _, s := range allNetStatus {
		apiNetStatus = append(apiNetStatus, infrav1.NetworkStatus{
			Connected:   s.Connected,
			IPAddrs:     sanitizeIPAddrs(&ctx.VMContext, s.IPAddrs),
			MACAddr:     s.MACAddr,
			NetworkName: s.NetworkName,
		})
	}
	return apiNetStatus, nil
}

func (vms *VMService) getBootstrapData(ctx *context.VMContext) ([]byte, error) {
	if ctx.ICSVM.Spec.BootstrapRef == nil {
		ctx.Logger.Info("VM has no bootstrap data")
		return nil, nil
	}

	secret := &corev1.Secret{}
	secretKey := apitypes.NamespacedName{
		Namespace: ctx.ICSVM.Spec.BootstrapRef.Namespace,
		Name:      ctx.ICSVM.Spec.BootstrapRef.Name,
	}
	if err := ctx.Client.Get(ctx, secretKey, secret); err != nil {
		return nil, errors.Wrapf(err, "failed to retrieve bootstrap data secret for %s", ctx)
	}

	value, ok := secret.Data["value"]
	if !ok {
		return nil, errors.New("error retrieving bootstrap data: secret value key is missing")
	}

	return value, nil
}

func (vms *VMService) reconcileDeleteIPAddress(ctx *context.VMContext) error {
	ipAddresses := &infrav1.IPAddressList{}
	err := ctx.Client.List(ctx, ipAddresses,
		ctrlclient.InNamespace(ctx.ICSVM.GetNamespace()),
		ctrlclient.MatchingFields{"spec.vmRef.name": ctx.ICSVM.GetName()},
	)
	if err != nil {
		ctx.Logger.Error(err,"fail to get k8s ipAddress resources.")
		return err
	}
	if ipAddresses.Items != nil {
		for _, ipAddress := range ipAddresses.Items {
			// If the IPAddress was found and it's not already enqueued for
			// deletion, go ahead and attempt to delete it.
			if err := ctx.Client.Delete(ctx, ipAddress.DeepCopy()); err != nil {
				return err
			}
		}

		// Go ahead and return here since the deletion of the ICSVM resource
		// will trigger a new reconcile for this ICSMachine resource.
		return nil
	}

	return nil
}