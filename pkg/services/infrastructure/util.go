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
	gocontext "context"
	"math/rand"
	"strings"
	"time"

	"github.com/inspur-ics/ics-go-sdk/client/types"
	taskapi "github.com/inspur-ics/ics-go-sdk/task"
	vmapi "github.com/inspur-ics/ics-go-sdk/vm"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/services/infrastructure/net"
)

func sanitizeIPAddrs(ctx *context.VMContext, ipAddrs []string) []string {
	if len(ipAddrs) == 0 {
		return nil
	}
	newIPAddrs := []string{}
	for _, addr := range ipAddrs {
		if err := net.ErrOnLocalOnlyIPAddr(addr); err != nil {
			ctx.Logger.V(4).Info("ignoring IP address", "reason", err.Error())
		} else {
			newIPAddrs = append(newIPAddrs, addr)
		}
	}
	return newIPAddrs
}

// findVM searches for a VM in one of two ways:
//   1. If the BIOS UUID is available, then it is used to find the VM.
//   2. Lacking the BIOS UUID, the VM is queried by its instance UUID,
//      which was assigned the value of the ICSVM resource's UID string.
//   3. If it is not found by instance UUID, fallback to an inventory path search
//      using the vm cluster path and the ICSVM name
func findVM(ctx *context.VMContext) (types.ManagedObjectReference, error) {
	virtualMachineService := vmapi.NewVirtualMachineService(ctx.Session.Client)
	if biosUUID := ctx.ICSVM.Spec.BiosUUID; biosUUID != "" {
		objRef, err := virtualMachineService.GetVMByUUID(ctx, biosUUID)
		if err != nil {
			return types.ManagedObjectReference{}, err
		}
		if objRef == nil {
			ctx.Logger.Info("vm not found by bios uuid", "biosuuid", biosUUID)
			return types.ManagedObjectReference{}, errNotFound{uuid: biosUUID}
		}
		reference := types.ManagedObjectReference{
			Type:  "id",
			Value: objRef.ID,
		}
		ctx.Logger.Info("vm found by bios uuid", "vmref", reference)
		return reference, nil
	}

	objRef := &types.VirtualMachine{}
	instanceUUID := ctx.ICSVM.Spec.UID
	if instanceUUID != "" {
		vmObj, err := virtualMachineService.GetVM(ctx, instanceUUID)
		if err != nil {
			ctx.Logger.Error(err,"fail to get vm by vm UUD")
		}
		objRef = vmObj
	} else {
		objRef = nil
	}
	if objRef == nil || objRef.ID == "" {
		vm, err := virtualMachineService.GetVMByName(ctx, ctx.ICSVM.Name)
		if err != nil {
			return types.ManagedObjectReference{}, errNotFound{byInventoryPath: ctx.ICSVM.Name}
		}
		if vm == nil || vm.ID == "" {
			return types.ManagedObjectReference{}, errNotFound{byInventoryPath: ctx.ICSVM.Name}
		}
		reference := types.ManagedObjectReference{
			Type: "id",
			Value: vm.ID,
		}
		ctx.Logger.Info("vm found by name", "vmref", reference)
		return reference, nil
	}
	reference := types.ManagedObjectReference{
		Type: "id",
		Value: objRef.ID,
	}
	ctx.Logger.Info("vm found by instance uuid", "vmref", reference)
	return reference, nil
}

func getTask(ctx *context.VMContext) *types.TaskInfo {
	if ctx.ICSVM.Status.TaskRef == "" {
		return nil
	}
	moRef := types.Task{
		TaskId:  ctx.ICSVM.Status.TaskRef,
	}
	taskService := taskapi.NewTaskService(ctx.Session.Client)
	obj, err := taskService.GetTaskInfo(ctx, &moRef)
	if err != nil {
		ctx.Logger.Error(err,"get ics task info error")
		return nil
	}
	return obj
}

func reconcileInFlightTask(ctx *context.VMContext) (bool, error) {
	// Check to see if there is an in-flight task.
	task := getTask(ctx)

	// If no task was found then make sure to clear the ICSVM
	// resource's Status.TaskRef field.
	if task == nil {
		ctx.ICSVM.Status.TaskRef = ""
		return false, nil
	}

	// Otherwise the course of action is determined by the state of the task.
	logger := ctx.Logger.WithName(task.Id)
	logger.Info("task found", "state", task.State, "task-id", task.Id)
	switch task.State {
	case "WAITING":
		return true, nil
	case "RUNNING":
		return true, nil
	case "READY":
		return true, nil
	case "FINISHED":
		ctx.ICSVM.Status.TaskRef = ""
		return false, nil
	case "ERROR":
		ctx.ICSVM.Status.TaskRef = ""
		return false, nil
	default:
		return false, errors.Errorf("unknown task state %q for %q", task.State, ctx)
	}
}

func reconcileICSVMWhenNetworkIsReady(
	ctx *virtualMachineContext,
	powerOnTask *types.Task) {

	reconcileICSVMOnChannel(
		&ctx.VMContext,
		func() (<-chan []interface{}, <-chan error, error) {

			// Wait for the VM to be powered on.
			taskService := taskapi.NewTaskService(ctx.Session.Client)
			powerOnTaskInfo, err := taskService.WaitForResult(ctx, powerOnTask)
			if err != nil && powerOnTaskInfo == nil {
				return nil, nil, errors.Wrapf(err, "failed to wait for power on op for vm %s", ctx)
			}
			vmInfo, err := ctx.Obj.GetVM(ctx, ctx.Ref.Value)
			if err != nil {
				return nil, nil, errors.Wrapf(err, "failed to get power state for vm %s", ctx)
			}
			if strings.Compare(vmInfo.Status,"STARTED") != 0 {
				return nil, nil, errors.Errorf(
					"unexpected power state %v for vm %s",
					vmInfo.Status, ctx)
			}

			// Get all the MAC addresses. This is done separately from waiting
			// for all NICs to have MAC addresses in order to ensure the order
			// of the retrieved MAC addresses matches the order of the device
			// specs, and not the propery change order.
			_, macToDeviceIndex, deviceToMacIndex, err := getMacAddresses(ctx)
			if err != nil {
				return nil, nil, errors.Wrapf(err, "failed to get mac addresses for vm %s", ctx)
			}

			// Wait for the IP addresses to show up for the VM.
			chanIPAddresses, chanErrs := waitForIPAddresses(ctx, macToDeviceIndex, deviceToMacIndex)

			// Trigger a reconcile every time a new IP is discovered.
			chanOfLoggerKeysAndValues := make(chan []interface{})
			go func() {
				for ip := range chanIPAddresses {
					chanOfLoggerKeysAndValues <- []interface{}{
						"reason", "network",
						"ipAddress", ip,
					}
				}
			}()
			return chanOfLoggerKeysAndValues, chanErrs, nil
		})
}

func reconcileICSVMOnTaskCompletion(ctx *context.VMContext) {
	task := getTask(ctx)
	if task == nil {
		ctx.Logger.V(4).Info(
			"skipping reconcile ICSVM on task completion",
			"reason", "no-task")
		return
	}
	taskRef := task.Id
	taskHelper := taskapi.NewTaskService(ctx.Session.Client)

	ctx.Logger.Info(
		"enqueuing reconcile request on task completion",
		"task-ref", taskRef,
		"task-name", task.Name,
		"task-entity-name", task.TargetName,
		"task-description-id", task.ProcessId)

	reconcileICSVMOnFuncCompletion(ctx, func() ([]interface{}, error) {
		ref := types.Task{
			TaskId: taskRef,
		}
		taskInfo, err := taskHelper.WaitForResult(ctx, &ref)

		// An error is only returned if the process of waiting for the result
		// failed, *not* if the task itself failed.
		if err != nil {
			return nil, err
		}

		return []interface{}{
			"reason", "task",
			"task-ref", taskRef,
			"task-name", taskInfo.Name,
			"task-entity-name", taskInfo.TargetName,
			"task-state", taskInfo.State,
			"task-description-id", taskInfo.ProcessId,
		}, nil
	})
}

func reconcileICSVMOnFuncCompletion(
	ctx *context.VMContext,
	waitFn func() (loggerKeysAndValues []interface{}, _ error)) {

	obj := ctx.ICSVM.DeepCopy()
	gvk := obj.GetObjectKind().GroupVersionKind()

	// Wait on the function to complete in a background goroutine.
	go func() {
		loggerKeysAndValues, err := waitFn()
		if err != nil {
			ctx.Logger.Error(err, "failed to wait on func")
			return
		}

		// Once the task has completed (successfully or otherwise), trigger
		// a reconcile event for the associated resource by sending a
		// GenericEvent into the event channel for the resource type.
		ctx.Logger.Info("triggering GenericEvent", loggerKeysAndValues...)
		eventChannel := ctx.GetGenericEventChannelFor(gvk)
		eventChannel <- event.GenericEvent{
			Meta:   obj,
			Object: obj,
		}
	}()
}

func reconcileICSVMOnChannel(
	ctx *context.VMContext,
	waitFn func() (<-chan []interface{}, <-chan error, error)) {

	obj := ctx.ICSVM.DeepCopy()
	gvk := obj.GetObjectKind().GroupVersionKind()

	// Send a generic event for every set of logger keys/values received
	// on the channel.
	go func() {
		chanOfLoggerKeysAndValues, chanErrs, err := waitFn()
		if err != nil {
			ctx.Logger.Error(err, "failed to wait on func")
			return
		}
		for {
			select {
			case loggerKeysAndValues := <-chanOfLoggerKeysAndValues:
				if loggerKeysAndValues == nil {
					return
				}
				go func() {
					// Trigger a reconcile event for the associated resource by
					// sending a GenericEvent into the event channel for the resource
					// type.
					ctx.Logger.Info("triggering GenericEvent", loggerKeysAndValues...)
					eventChannel := ctx.GetGenericEventChannelFor(gvk)
					eventChannel <- event.GenericEvent{
						Meta:   obj,
						Object: obj,
					}
				}()
			case err := <-chanErrs:
				if err != nil {
					ctx.Logger.Error(err, "error occurred while waiting to trigger a generic event")
				}
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// getMacAddresses gets the MAC addresses for all network devices.
// This happens separately from waitForMacAddresses to ensure returned order of
// devices matches the spec and not order in which the propery changes were
// noticed.
func getMacAddresses(ctx *virtualMachineContext) ([]string, map[string]int, map[int]string, error) {
	var (
		vm                   types.VirtualMachine
		macAddresses         []string
		macToDeviceSpecIndex = map[string]int{}
		deviceSpecIndexToMac = map[int]string{}
	)
	vmInfo, err := ctx.Obj.GetVM(ctx, ctx.Ref.Value)
	if err != nil {
		return nil, nil, nil, err
	}
	vm = *vmInfo
	i := 0
	for _, device := range vm.Nics {
		mac := device.Mac
		macAddresses = append(macAddresses, mac)
		macToDeviceSpecIndex[mac] = i
		deviceSpecIndexToMac[i] = mac
		i++
	}
	return macAddresses, macToDeviceSpecIndex, deviceSpecIndexToMac, nil
}

// waitForIPAddresses waits for all network devices that should be getting an
// IP address to have an IP address. This is any network device that specifies a
// network name and DHCP for v4 or v6 or one or more static IP addresses.
// The gocyclo detector is disabled for this function as it is difficult to
// rewrite muchs simpler due to the maps used to track state and the lambdas
// that use the maps.
// nolint:gocyclo,gocognit
func waitForIPAddresses(
	ctx *virtualMachineContext,
	macToDeviceIndex map[string]int,
	deviceToMacIndex map[int]string) (<-chan string, <-chan error) {

	var (
		chanErrs          = make(chan error)
		chanIPAddresses   = make(chan string)
		macToHasStaticIP  = map[string]map[string]struct{}{}
		virtualMachineService     = vmapi.NewVirtualMachineService(ctx.Session.Client)
	)

	// Initialize the nested maps early.
	for mac := range macToDeviceIndex {
		macToHasStaticIP[mac] = map[string]struct{}{}
	}

	onNicChange := func(nicChanges []types.Nic) bool {
		for _, nic := range nicChanges {
			mac := nic.Mac
			if mac == "" || nic.IP == "" {
				continue
			}
			// Ignore any that don't correspond to a network
			// device spec.
			deviceSpecIndex, ok := macToDeviceIndex[mac]
			if !ok {
				chanErrs <- errors.Errorf("unknown device spec index for mac %s while waiting for ip addresses for vm %s", mac, ctx)
				// Return true to stop the property collector from waiting
				// on any more changes.
				return true
			}
			if deviceSpecIndex < 0 || deviceSpecIndex >= len(ctx.ICSVM.Spec.Network.Devices) {
				chanErrs <- errors.Errorf("invalid device spec index %d for mac %s while waiting for ip addresses for vm %s", deviceSpecIndex, mac, ctx)
				// Return true to stop the property collector from waiting
				// on any more changes.
				return true
			}

			// Look at each IP and determine whether or not a reconcile has
			// been triggered for the IP.
			discoveredIP := nic.IP

			// If it's a static IP then check to see if the IP has
			// triggered a reconcile yet.
			if _, ok := macToHasStaticIP[mac][discoveredIP]; !ok {
				// No reconcile yet. Record the IP send it to the
				// channel.
				ctx.Logger.Info(
					"discovered IP address",
					"addressType", "static",
					"addressValue", discoveredIP)
				macToHasStaticIP[mac][discoveredIP] = struct{}{}
				chanIPAddresses <- discoveredIP
			}
		}

		// Determine whether or not the wait operation is over by whether
		// or not the VM has all of the requested IP addresses.
		for index := range ctx.ICSVM.Spec.Network.Devices {
			_, ok := deviceToMacIndex[index]
			if !ok {
				chanErrs <- errors.Errorf("invalid mac index %d waiting for ip addresses for vm %s", index, ctx)

				// Return true to stop the property collector from waiting
				// on any more changes.
				return true
			}
		}

		ctx.Logger.Info("the VM has all of the requested IP addresses")
		return true
	}

	// The wait function will not return true until all the VM's
	// network devices have IP assignments that match the requested
	// network devie specs. However, every time a new IP is discovered,
	// a reconcile request will be triggered for the ICSVM.
	go func() {
		if err := Wait(ctx, virtualMachineService, ctx.Ref,
			onNicChange); err != nil {

			chanErrs <- errors.Wrapf(err, "failed to wait for ip addresses for vm %s", ctx)
		}
		close(chanIPAddresses)
		close(chanErrs)
	}()

	return chanIPAddresses, chanErrs
}

func Wait(ctx gocontext.Context, c *vmapi.VirtualMachineService, obj types.ManagedObjectReference, f func([]types.Nic) bool) error {
	return WaitForUpdates(ctx, c, obj, func(updates []types.Nic) bool {
		if f(updates) {
			return true
		}
		return false
	})
}

func WaitForUpdates(ctx gocontext.Context, c *vmapi.VirtualMachineService, obj types.ManagedObjectReference, f func([]types.Nic) bool) error {
	startTime := time.Now()
	_, err := c.GetVM(ctx, obj.Value)
	if err != nil {
		return err
	}

	for {
		vmInfo, err := c.GetVM(ctx, obj.Value)
		if err != nil {
			return err
		}
		if checkVMNetworkReady(vmInfo.Nics) {
			// Retry if the result came back empty
			waitingTime := rand.Int31n(1200)
			time.Sleep(time.Duration(waitingTime) * time.Millisecond)
			continue
		}
		if f(vmInfo.Nics) {
			return nil
		}

		if time.Now().Sub(startTime) > time.Duration(900) {
			return errors.Errorf("Get VM %s IP config error, through vmtools.", vmInfo.Name)
		}
	}
}

func checkVMNetworkReady(nics []types.Nic) bool {
	status := true
	for _, nic := range nics {
		ip := nic.IP
		if &ip != nil {
			status = false
			break
		}
	}
	return status
}
