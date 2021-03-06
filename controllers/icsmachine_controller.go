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

package controllers

import (
	goctx "context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	apitypes "k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	clusterutilv1 "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1alpha3"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/record"
	infrautilv1 "github.com/inspur-ics/cluster-api-provider-ics/pkg/util"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=icsmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=icsmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch

// AddMachineControllerToManager adds the machine controller to the provided
// manager.
func AddMachineControllerToManager(ctx *context.ControllerManagerContext, mgr manager.Manager) error {

	var (
		controlledType     = &infrav1.ICSMachine{}
		controlledTypeName = reflect.TypeOf(controlledType).Elem().Name()
		controlledTypeGVK  = infrav1.GroupVersion.WithKind(controlledTypeName)

		controllerNameShort = fmt.Sprintf("%s-controller", strings.ToLower(controlledTypeName))
		controllerNameLong  = fmt.Sprintf("%s/%s/%s", ctx.Namespace, ctx.Name, controllerNameShort)
	)

	// Build the controller context.
	controllerContext := &context.ControllerContext{
		ControllerManagerContext: ctx,
		Name:                     controllerNameShort,
		Recorder:                 record.New(mgr.GetEventRecorderFor(controllerNameLong)),
		Logger:                   ctx.Logger.WithName(controllerNameShort),
	}

	r := machineReconciler{ControllerContext: controllerContext}

	controller, err := ctrl.NewControllerManagedBy(mgr).
		// Watch the controlled, infrastructure resource.
		For(controlledType).
		// Watch any ICSVM resources owned by the controlled type.
		Watches(
			&source.Kind{Type: &infrav1.ICSVM{}},
			&handler.EnqueueRequestForOwner{OwnerType: controlledType, IsController: false},
		).
		// Watch the CAPI resource that owns this infrastructure resource.
		Watches(
			&source.Kind{Type: &clusterv1.Machine{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: clusterutilv1.MachineToInfrastructureMapFunc(controlledTypeGVK),
			},
		).
		// Watch a GenericEvent channel for the controlled resource.
		//
		// This is useful when there are events outside of Kubernetes that
		// should cause a resource to be synchronized, such as a goroutine
		// waiting on some asynchronous, external task to complete.
		Watches(
			&source.Channel{Source: ctx.GetGenericEventChannelFor(controlledTypeGVK)},
			&handler.EnqueueRequestForObject{},
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctx.MaxConcurrentReconciles}).
		Build(r)
	if err != nil {
		return err
	}

	err = controller.Watch(
		&source.Kind{Type: &clusterv1.Cluster{}},
		&handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(r.clusterToICSMachines),
		},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldCluster := e.ObjectOld.(*clusterv1.Cluster)
				newCluster := e.ObjectNew.(*clusterv1.Cluster)
				return oldCluster.Spec.Paused && !newCluster.Spec.Paused
			},
			CreateFunc: func(e event.CreateEvent) bool {
				if _, ok := e.Meta.GetAnnotations()[clusterv1.PausedAnnotation]; !ok {
					return false
				}
				return true
			},
		})
	if err != nil {
		return err
	}
	return nil
}

type machineReconciler struct {
	*context.ControllerContext
}

// Reconcile ensures the back-end state reflects the Kubernetes resource state intent.
func (r machineReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, reterr error) {

	// Get the ICSMachine resource for this request.
	icsMachine := &infrav1.ICSMachine{}
	if err := r.Client.Get(r, req.NamespacedName, icsMachine); err != nil {
		if apierrors.IsNotFound(err) {
			r.Logger.Info("ICSMachine not found, won't reconcile", "key", req.NamespacedName)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Fetch the CAPI Machine.
	machine, err := clusterutilv1.GetOwnerMachine(r, r.Client, icsMachine.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if machine == nil {
		r.Logger.Info("Waiting for Machine Controller to set OwnerRef on ICSMachine")
		return reconcile.Result{}, nil
	}

	// Fetch the CAPI Cluster.
	cluster, err := clusterutilv1.GetClusterFromMetadata(r, r.Client, machine.ObjectMeta)
	if err != nil {
		r.Logger.Info("Machine is missing cluster label or cluster does not exist")
		return reconcile.Result{}, nil
	}
	if clusterutilv1.IsPaused(cluster, icsMachine) {
		r.Logger.V(4).Info("ICSMachine %s/%s linked to a cluster that is paused",
			icsMachine.Namespace, icsMachine.Name)
		return reconcile.Result{}, nil
	}

	// Fetch the ICSCluster
	icsCluster := &infrav1.ICSCluster{}
	icsClusterName := client.ObjectKey{
		Namespace: icsMachine.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	if err := r.Client.Get(r, icsClusterName, icsCluster); err != nil {
		r.Logger.Info("Waiting for ICSCluster")
		return reconcile.Result{}, nil
	}

	// Create the patch helper.
	patchHelper, err := patch.NewHelper(icsMachine, r.Client)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(
			err,
			"failed to init patch helper for %s %s/%s",
			icsMachine.GroupVersionKind(),
			icsMachine.Namespace,
			icsMachine.Name)
	}

	// Create the machine context for this request.
	machineContext := &context.MachineContext{
		ControllerContext: r.ControllerContext,
		Cluster:           cluster,
		ICSCluster:        icsCluster,
		Machine:           machine,
		ICSMachine:        icsMachine,
		Logger:            r.Logger.WithName(req.Namespace).WithName(req.Name),
		PatchHelper:       patchHelper,
	}

	// Always issue a patch when exiting this function so changes to the
	// resource are patched back to the API server.
	defer func() {
		// Patch the ICSMachine resource.
		if err := machineContext.Patch(); err != nil {
			if reterr == nil {
				reterr = err
			}
			machineContext.Logger.Error(err, "patch failed", "machine", machineContext.String())
		}
	}()

	// Handle deleted machines
	if !icsMachine.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(machineContext)
	}

	// Handle non-deleted machines
	return r.reconcileNormal(machineContext)
}

func (r machineReconciler) reconcileDelete(ctx *context.MachineContext) (reconcile.Result, error) {
	ctx.Logger.Info("Handling deleted ICSMachine")

	if err := r.reconcileDeleteVM(ctx); err != nil {
		if apierrors.IsNotFound(err) {
			// The VM is deleted so remove the finalizer.
			ctrlutil.RemoveFinalizer(ctx.ICSMachine, infrav1.MachineFinalizer)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}
	ctx.Logger.Info("Waiting for ICSVM to be deleted")
	return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r machineReconciler) reconcileDeleteVM(ctx *context.MachineContext) error {
	return r.reconcileDeleteVMPre7(ctx)
}

func (r machineReconciler) reconcileDeleteVMPre7(ctx *context.MachineContext) error {
	vm, err := r.findVMPre7(ctx)
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

		// Go ahead and return here since the deletion of the ICSVM resource
		// will trigger a new reconcile for this ICSMachine resource.
		return nil
	}

	return nil
}

func (r machineReconciler) findVMPre7(ctx *context.MachineContext) (*infrav1.ICSVM, error) {
	// Get ready to find the associated ICSVM resource.
	vm := &infrav1.ICSVM{}
	vmKey := apitypes.NamespacedName{
		Namespace: ctx.ICSMachine.Namespace,
		Name:      ctx.Machine.Name,
	}
	// Attempt to find the associated ICSVM resource.
	if err := ctx.Client.Get(ctx, vmKey, vm); err != nil {
		return nil, err
	}
	return vm, nil
}

func (r machineReconciler) reconcileNormal(ctx *context.MachineContext) (reconcile.Result, error) {
	icsVM, err := r.findVMPre7(ctx)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
	}
	if icsVM != nil {
		// Reconcile ICSMachine's failures
		ctx.ICSMachine.Status.FailureReason = icsVM.Status.FailureReason
		ctx.ICSMachine.Status.FailureMessage = icsVM.Status.FailureMessage
	}

	// If the ICSMachine is in an error state, return early.
	if ctx.ICSMachine.Status.FailureReason != nil || ctx.ICSMachine.Status.FailureMessage != nil {
		ctx.Logger.Info("Error state detected, skipping reconciliation")
		return reconcile.Result{}, nil
	}

	// If the ICSMachine doesn't have our finalizer, add it.
	ctrlutil.AddFinalizer(ctx.ICSMachine, infrav1.MachineFinalizer)

	if !ctx.Cluster.Status.InfrastructureReady {
		ctx.Logger.Info("Cluster infrastructure is not ready yet")
		return reconcile.Result{}, nil
	}

	// Make sure bootstrap data is available and populated.
	if ctx.Machine.Spec.Bootstrap.DataSecretName == nil {
		ctx.Logger.Info("Waiting for bootstrap data to be available")
		return reconcile.Result{}, nil
	}

	vm, err := r.reconcileNormalPre7(ctx, icsVM)
	if err != nil {
		r.Logger.Error(err,"reconcile normal error")
		if apierrors.IsAlreadyExists(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Convert the VM resource to unstructured data.
	vmData, err := runtime.DefaultUnstructuredConverter.ToUnstructured(vm)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err,
			"failed to convert %s to unstructured data",
			vm.GetObjectKind().GroupVersionKind().String())
	}
	vmObj := &unstructured.Unstructured{Object: vmData}
	vmObj.SetGroupVersionKind(vm.GetObjectKind().GroupVersionKind())
	vmObj.SetAPIVersion(vm.GetObjectKind().GroupVersionKind().GroupVersion().String())
	vmObj.SetKind(vm.GetObjectKind().GroupVersionKind().Kind)

	// Reconcile the ICSMachine's provider ID using the VM's BIOS UUID.
	if ok, err := r.reconcileProviderID(ctx, vmObj); !ok {
		if err != nil {
			return reconcile.Result{}, errors.Wrapf(err,
				"unexpected error while reconciling provider ID for %s", ctx)
		}
		ctx.Logger.Info("provider ID is not reconciled")
		return reconcile.Result{}, nil
	}

	// Reconcile the ICSMachine's node addresses from the VM's IP addresses.
	if ok, err := r.reconcileNetwork(ctx, vmObj); !ok {
		if err != nil {
			return reconcile.Result{}, errors.Wrapf(err,
				"unexpected error while reconciling network for %s", ctx)
		}
		ctx.Logger.Info("network is not reconciled")
		return reconcile.Result{}, nil
	}

	// Reconcile the ICSMachine's ready state from the VM's ready state.
	if ok, err := r.reconcileReadyState(ctx, vmObj); !ok {
		if err != nil {
			return reconcile.Result{}, errors.Wrapf(err,
				"unexpected error while reconciling ready state for %s", ctx)
		}
		ctx.Logger.Info("ready state is not reconciled")
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}

func (r machineReconciler) reconcileNormalPre7(ctx *context.MachineContext, icsVM *infrav1.ICSVM) (runtime.Object, error) {
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
		//   1. From the ICSMachine.Spec (the DeepCopyInto above)
		//   2. From the ICSCluster.Spec.CloudProviderConfiguration.Workspace
		//   3. From the ICSCluster.Spec
		icsCloudConfig := ctx.ICSCluster.Spec.CloudProviderConfiguration.Workspace
		if vm.Spec.Server == "" {
			if vm.Spec.Server = icsCloudConfig.Server; vm.Spec.Server == "" {
				vm.Spec.Server = ctx.ICSCluster.Spec.Server
			}
		}
		if vm.Spec.Datacenter == "" {
			vm.Spec.Datacenter = icsCloudConfig.Datacenter
		}
		if vm.Spec.Datastore == "" {
			vm.Spec.Datastore = icsCloudConfig.Datastore
		}
		if vm.Spec.Cluster == "" {
			vm.Spec.Cluster = icsCloudConfig.Cluster
		}
		if vm.Spec.ResourcePool == "" {
			vm.Spec.ResourcePool = icsCloudConfig.ResourcePool
		}
		if icsVM != nil {
			vm.Spec.BiosUUID = icsVM.Spec.BiosUUID
		}
		return nil
	}
	if _, err := ctrlutil.CreateOrUpdate(ctx, ctx.Client, vm, mutateFn); err != nil {
		if apierrors.IsAlreadyExists(err) {
			ctx.Logger.Info("ICSVM already exists")
			return nil, err
		}
		ctx.Logger.Error(err, "failed to CreateOrUpdate ICSVM",
			"namespace", vm.Namespace, "name", vm.Name)
		return nil, err
	}

	return vm, nil
}

func (r machineReconciler) reconcileNetwork(ctx *context.MachineContext, vm *unstructured.Unstructured) (bool, error) {
	var errs []error
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
		var machineAddresses []clusterv1.MachineAddress
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

func (r machineReconciler) reconcileProviderID(ctx *context.MachineContext, vm *unstructured.Unstructured) (bool, error) {
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
		ctx.Logger.Info("spec.biosUUID not found",
			"vmGVK", vm.GroupVersionKind().String(),
			"vmNamespace", vm.GetNamespace(),
			"vmName", vm.GetName())
		return false, nil
	}
	if biosUUID == "" {
		ctx.Logger.Info("spec.biosUUID is empty",
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

func (r machineReconciler) reconcileReadyState(ctx *context.MachineContext, vm *unstructured.Unstructured) (bool, error) {
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
		ctx.Logger.Info("status.ready not found",
			"vmGVK", vm.GroupVersionKind().String(),
			"vmNamespace", vm.GetNamespace(),
			"vmName", vm.GetName())
		return false, nil
	}
	if !ready {
		ctx.Logger.Info("status.ready is false",
			"vmGVK", vm.GroupVersionKind().String(),
			"vmNamespace", vm.GetNamespace(),
			"vmName", vm.GetName())
		return false, nil
	}

	ctx.ICSMachine.Status.Ready = true
	return true, nil
}

func (r *machineReconciler) clusterToICSMachines(a handler.MapObject) []reconcile.Request {
	requests := []reconcile.Request{}
	machines, err := infrautilv1.GetMachinesInCluster(goctx.Background(), r.Client, a.Meta.GetNamespace(), a.Meta.GetName())
	if err != nil {
		return requests
	}
	for _, m := range machines {
		r := reconcile.Request{
			NamespacedName: apitypes.NamespacedName{
				Name:      m.Name,
				Namespace: m.Namespace,
			},
		}
		requests = append(requests, r)
	}
	return requests
}
