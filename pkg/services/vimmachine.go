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

package services

import (
	goctx "context"
	"encoding/json"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	clusterutilv1 "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	infrautilv1 "github.com/inspur-ics/cluster-api-provider-ics/pkg/util"
)

type VimMachineService struct{}

func (v *VimMachineService) FetchICSMachine(c client.Client, name types.NamespacedName) (context.MachineContext, error) {
	icsMachine := &infrav1.ICSMachine{}
	err := c.Get(goctx.Background(), name, icsMachine)

	return &context.VIMMachineContext{ICSMachine: icsMachine}, err
}

func (v *VimMachineService) FetchICSCluster(c client.Client, cluster *clusterv1.Cluster, machineContext context.MachineContext) (context.MachineContext, error) {
	ctx, ok := machineContext.(*context.VIMMachineContext)
	if !ok {
		return nil, errors.New("received unexpected VIMMachineContext type")
	}
	icsCluster := &infrav1.ICSCluster{}
	icsClusterName := client.ObjectKey{
		Namespace: machineContext.GetObjectMeta().Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	err := c.Get(goctx.Background(), icsClusterName, icsCluster)

	ctx.ICSCluster = icsCluster
	return ctx, err
}

func (v *VimMachineService) ReconcileDelete(c context.MachineContext) error {
	ctx, ok := c.(*context.VIMMachineContext)
	if !ok {
		return errors.New("received unexpected VIMMachineContext type")
	}

	vm, err := v.getICSVm(ctx)
	// Attempt to find the associated ICSVM resource.
	if err != nil {
		return err
	}

	if vm != nil && vm.GetDeletionTimestamp().IsZero() {
		// If the ICSVM was found and it's not already enqueued for
		// deletion, go ahead and attempt to delete it.
		if err := ctx.Client.Delete(ctx, vm); err != nil {
			return err
		}
	}

	// ICSMachine wraps a VMSphereVM, so we are mirroring status from the underlying VMSphereVM
	// in order to provide evidences about machine deletion.
	conditions.SetMirror(ctx.ICSMachine, infrav1.VMProvisionedCondition, vm)
	return nil
}

func (v *VimMachineService) SyncFailureReason(c context.MachineContext) (bool, error) {
	ctx, ok := c.(*context.VIMMachineContext)
	if !ok {
		return false, errors.New("received unexpected VIMMachineContext type")
	}

	icsVM, err := v.getICSVm(ctx)
	if err != nil {
		return false, err
	}
	if icsVM != nil {
		// Reconcile ICSMachine's failures
		ctx.ICSMachine.Status.FailureReason = icsVM.Status.FailureReason
		ctx.ICSMachine.Status.FailureMessage = icsVM.Status.FailureMessage
	}

	return ctx.ICSMachine.Status.FailureReason != nil || ctx.ICSMachine.Status.FailureMessage != nil, err
}

func (v *VimMachineService) ReconcileNormal(c context.MachineContext) (bool, error) {
	ctx, ok := c.(*context.VIMMachineContext)
	if !ok {
		return false, errors.New("received unexpected VIMMachineContext type")
	}
	icsVM, err := v.getICSVm(ctx)
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}

	vm, err := v.createOrPatchICSVM(ctx, icsVM)
	if err != nil {
		ctx.Logger.Error(err, "error creating or patching VM", "icsVM", icsVM)
		return false, err
	}

	// Convert the VM resource to unstructured data.
	vmData, err := runtime.DefaultUnstructuredConverter.ToUnstructured(vm)
	if err != nil {
		return false, errors.Wrapf(err,
			"failed to convert %s to unstructured data",
			vm.GetObjectKind().GroupVersionKind().String())
	}
	vmObj := &unstructured.Unstructured{Object: vmData}
	vmObj.SetGroupVersionKind(vm.GetObjectKind().GroupVersionKind())
	vmObj.SetAPIVersion(vm.GetObjectKind().GroupVersionKind().GroupVersion().String())
	vmObj.SetKind(vm.GetObjectKind().GroupVersionKind().Kind)

	// Waits the VM's ready state.
	if ok, err := v.waitReadyState(ctx, vmObj); !ok {
		if err != nil {
			return false, errors.Wrapf(err, "unexpected error while reconciling ready state for %s", ctx)
		}
		// ctx.Logger.Info("waiting for ready state")
		// ICSMachine wraps a VMSphereVM, so we are mirroring status from the underlying VMSphereVM
		// in order to provide evidences about machine provisioning while provisioning is actually happening.
		conditions.SetMirror(ctx.ICSMachine, infrav1.VMProvisionedCondition, conditions.UnstructuredGetter(vmObj))
		return true, nil
	}

	// Reconcile the ICSMachine's provider ID using the VM's BIOS UUID.
	if ok, err := v.reconcileProviderID(ctx, vmObj); !ok {
		if err != nil {
			return false, errors.Wrapf(err, "unexpected error while reconciling provider ID for %s", ctx)
		}
		ctx.Logger.Info("provider ID is not reconciled")
		return true, nil
	}

	// Reconcile the ICSMachine's node addresses from the VM's IP addresses.
	if ok, err := v.reconcileNetwork(ctx, vmObj); !ok {
		if err != nil {
			return false, errors.Wrapf(err, "unexpected error while reconciling network for %s", ctx)
		}
		ctx.Logger.Info("network is not reconciled")
		conditions.MarkFalse(ctx.ICSMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForNetworkAddressesReason, clusterv1.ConditionSeverityInfo, "")
		return true, nil
	}

	ctx.ICSMachine.Status.Ready = true
	return false, nil
}

func (v *VimMachineService) GetHostInfo(c context.MachineContext) (string, error) {
	ctx, ok := c.(*context.VIMMachineContext)
	if !ok {
		return "", errors.New("received unexpected VIMMachineContext type")
	}

	icsVM := &infrav1.ICSVM{}
	if err := ctx.Client.Get(ctx, client.ObjectKey{
		Namespace: ctx.Machine.Namespace,
		Name:      ctx.Machine.Name,
	}, icsVM); err != nil {
		return "", err
	}

	if conditions.IsTrue(icsVM, infrav1.VMProvisionedCondition) {
		return icsVM.Status.Host, nil
	}
	ctx.Logger.V(4).Info("VMProvisionedCondition is set to false", "icsVM", icsVM.Name)
	return "", nil
}

func (v *VimMachineService) getICSVm(ctx *context.VIMMachineContext) (*infrav1.ICSVM, error) {
	// Get ready to find the associated ICSVM resource.
	vm := &infrav1.ICSVM{}
	vmKey := types.NamespacedName{
		Namespace: ctx.ICSMachine.Namespace,
		Name:      ctx.Machine.Name,
	}
	// Attempt to find the associated ICSVM resource.
	if err := ctx.Client.Get(ctx, vmKey, vm); err != nil {
		return nil, err
	}
	return vm, nil
}

func (v *VimMachineService) waitReadyState(ctx *context.VIMMachineContext, vm *unstructured.Unstructured) (bool, error) {
	ready, ok, err := unstructured.NestedBool(vm.Object, "status", "ready")
	if !ok {
		if err != nil {
			return false, errors.Wrapf(err,
				"unexpected error when getting status.ready from %s %s/%s for %s",
				vm.GroupVersionKind(),
				vm.GetNamespace(),
				vm.GetName(),
				ctx)
		}
		return false, nil
	}
	if !ready {
		ctx.Logger.Info("ICS VM is not ready",
			"vmGVK", vm.GroupVersionKind().String(),
			"vmNamespace", vm.GetNamespace(),
			"vmName", vm.GetName())
		return false, nil
	}

	return true, nil
}

func (v *VimMachineService) reconcileProviderID(ctx *context.VIMMachineContext, vm *unstructured.Unstructured) (bool, error) {
	biosUUID, ok, err := unstructured.NestedString(vm.Object, "spec", "biosUUID")
	if !ok {
		if err != nil {
			return false, errors.Wrapf(err,
				"unexpected error when getting spec.biosUUID from %s %s/%s for %s",
				vm.GroupVersionKind(),
				vm.GetNamespace(),
				vm.GetName(),
				ctx)
		}
		ctx.Logger.Info("ICS VM biosUUID not found",
			"vmGVK", vm.GroupVersionKind().String(),
			"vmNamespace", vm.GetNamespace(),
			"vmName", vm.GetName())
		return false, nil
	}
	if biosUUID == "" {
		ctx.Logger.Info("ICS VM biosUUID is empty",
			"vmGVK", vm.GroupVersionKind().String(),
			"vmNamespace", vm.GetNamespace(),
			"vmName", vm.GetName())
		return false, nil
	}

	providerID := infrautilv1.ConvertUUIDToProviderID(biosUUID)
	if providerID == "" {
		return false, errors.Errorf("invalid BIOS UUID %s from %s %s/%s for %s",
			biosUUID,
			vm.GroupVersionKind(),
			vm.GetNamespace(),
			vm.GetName(),
			ctx)
	}
	if ctx.ICSMachine.Spec.ProviderID == nil || *ctx.ICSMachine.Spec.ProviderID != providerID {
		ctx.ICSMachine.Spec.ProviderID = &providerID
		ctx.Logger.Info("updated provider ID", "provider-id", providerID)
	}

	return true, nil
}

//nolint:nestif
func (v *VimMachineService) reconcileNetwork(ctx *context.VIMMachineContext, vm *unstructured.Unstructured) (bool, error) {
	errs := []error{}
	if networkStatusListOfIfaces, ok, _ := unstructured.NestedSlice(vm.Object, "status", "network"); ok {
		networkStatusList := []infrav1.NetworkStatus{}
		for i, networkStatusListMemberIface := range networkStatusListOfIfaces {
			if buf, err := json.Marshal(networkStatusListMemberIface); err != nil {
				ctx.Logger.Error(err,
					"unsupported data for member of status.network list",
					"index", i)
				errs = append(errs, err)
			} else {
				var networkStatus infrav1.NetworkStatus
				err := json.Unmarshal(buf, &networkStatus)
				if err == nil && networkStatus.MACAddr == "" {
					err = errors.New("macAddr is required")
					errs = append(errs, err)
				}
				if err != nil {
					ctx.Logger.Error(err,
						"unsupported data for member of status.network list",
						"index", i, "data", string(buf))
					errs = append(errs, err)
				} else {
					networkStatusList = append(networkStatusList, networkStatus)
				}
			}
		}
		ctx.ICSMachine.Status.Network = networkStatusList
	}

	if addresses, ok, _ := unstructured.NestedStringSlice(vm.Object, "status", "addresses"); ok {
		machineAddresses := []clusterv1.MachineAddress{}
		for _, addr := range addresses {
			machineAddresses = append(machineAddresses, clusterv1.MachineAddress{
				Type:    clusterv1.MachineExternalIP,
				Address: addr,
			})
		}
		ctx.ICSMachine.Status.Addresses = machineAddresses
	}

	if len(ctx.ICSMachine.Status.Addresses) == 0 {
		ctx.Logger.Info("waiting on IP addresses")
		return false, kerrors.NewAggregate(errs)
	}

	return true, nil
}

func (v *VimMachineService) createOrPatchICSVM(ctx *context.VIMMachineContext, icsVM *infrav1.ICSVM) (runtime.Object, error) {
	// Create or update the ICSVM resource.
	vm := &infrav1.ICSVM{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ctx.ICSMachine.Namespace,
			Name:      ctx.Machine.Name,
		},
	}
	mutateFn := func() (err error) {
		// Ensure the ICSMachine is marked as an owner of the ICSVM.
		vm.SetOwnerReferences(clusterutilv1.EnsureOwnerRef(
			vm.OwnerReferences,
			metav1.OwnerReference{
				APIVersion: ctx.ICSMachine.APIVersion,
				Kind:       ctx.ICSMachine.Kind,
				Name:       ctx.ICSMachine.Name,
				UID:        ctx.ICSMachine.UID,
			}))

		// Instruct the ICSVM to use the CAPI bootstrap data resource.
		// BootstrapRef field should be replaced with BootstrapSecret of type string
		vm.Spec.BootstrapRef = &corev1.ObjectReference{
			APIVersion: "v1",
			Kind:       "Secret",
			Name:       *ctx.Machine.Spec.Bootstrap.DataSecretName,
			Namespace:  ctx.Machine.ObjectMeta.Namespace,
		}

		// Initialize the ICSVM's labels map if it is nil.
		if vm.Labels == nil {
			vm.Labels = map[string]string{}
		}

		// Ensure the ICSVM has a label that can be used when searching for
		// resources associated with the target cluster.
		vm.Labels[clusterv1.ClusterLabelName] = ctx.Machine.Labels[clusterv1.ClusterLabelName]

		// For convenience, add a label that makes it easy to figure out if the
		// ICSVM resource is part of some control plane.
		if val, ok := ctx.Machine.Labels[clusterv1.MachineControlPlaneLabelName]; ok {
			vm.Labels[clusterv1.MachineControlPlaneLabelName] = val
		}

		// Copy the ICSMachine's VM clone spec into the ICSVM's
		// clone spec.
		ctx.ICSMachine.Spec.VirtualMachineCloneSpec.DeepCopyInto(&vm.Spec.VirtualMachineCloneSpec)

		// Several of the ICSVM's clone spec properties can be derived
		// from multiple places. The order is:
		//
		//   1. From the Machine.Spec.FailureDomain
		//   2. From the ICSMachine.Spec (the DeepCopyInto above)
		//   3. From the ICSCluster.Spec
		if vm.Spec.CloudName == "" {
			vm.Spec.CloudName = ctx.ICSCluster.Spec.CloudName
		}
		if icsVM != nil {
			vm.Spec.BiosUUID = icsVM.Spec.BiosUUID
		}
		return nil
	}

	vmKey := types.NamespacedName{
		Namespace: vm.Namespace,
		Name:      vm.Name,
	}
	result, err := ctrlutil.CreateOrPatch(ctx, ctx.Client, vm, mutateFn)
	if err != nil {
		ctx.Logger.Error(
			err,
			"failed to CreateOrPatch ICSVM",
			"namespace",
			vm.Namespace,
			"name",
			vm.Name,
		)
		return nil, err
	}
	switch result {
	case ctrlutil.OperationResultNone:
		ctx.Logger.Info(
			"no update required for vm",
			"vm",
			vmKey,
		)
	case ctrlutil.OperationResultCreated:
		ctx.Logger.Info(
			"created vm",
			"vm",
			vmKey,
		)
	case ctrlutil.OperationResultUpdated:
		ctx.Logger.Info(
			"updated vm",
			"vm",
			vmKey,
		)
	case ctrlutil.OperationResultUpdatedStatus:
		ctx.Logger.Info(
			"updated vm and vm status",
			"vm",
			vmKey,
		)
	case ctrlutil.OperationResultUpdatedStatusOnly:
		ctx.Logger.Info(
			"updated vm status",
			"vm",
			vmKey,
		)
	}

	return vm, nil
}
