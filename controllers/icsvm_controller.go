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

package controllers

import (
	goctx "context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/inspur-ics/cluster-api-provider-ics/pkg/clustermodule"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/identity"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/services"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/session"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	clusterutilv1 "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/record"
	basev1 "github.com/inspur-ics/cluster-api-provider-ics/pkg/services/goclient"
	infrautilv1 "github.com/inspur-ics/cluster-api-provider-ics/pkg/util"
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=icsvms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=icsvms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinedeployments;machinesets,verbs=get;list;watch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=kubeadmcontrolplanes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch

// AddVMControllerToManager adds the VM controller to the provided manager.
//nolint:forcetypeassert
func AddVMControllerToManager(ctx *context.ControllerManagerContext, mgr manager.Manager) error {
	var (
		controlledType     = &infrav1.ICSVM{}
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
	r := vmReconciler{ControllerContext: controllerContext}
	controller, err := ctrl.NewControllerManagedBy(mgr).
		// Watch the controlled, infrastructure resource.
		For(controlledType).
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
		handler.EnqueueRequestsFromMapFunc(r.getClusterToICSVMsReq),
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldCluster := e.ObjectOld.(*clusterv1.Cluster)
				newCluster := e.ObjectNew.(*clusterv1.Cluster)
				return oldCluster.Spec.Paused && !newCluster.Spec.Paused
			},
			CreateFunc: func(e event.CreateEvent) bool {
				if _, ok := e.Object.GetAnnotations()[clusterv1.PausedAnnotation]; !ok {
					return false
				}
				return true
			},
		})
	if err != nil {
		return err
	}

	err = controller.Watch(
		&source.Kind{Type: &infrav1.ICSCluster{}},
		handler.EnqueueRequestsFromMapFunc(r.getICSClusterToICSVMsReq),
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldCluster := e.ObjectOld.(*infrav1.ICSCluster)
				newCluster := e.ObjectNew.(*infrav1.ICSCluster)
				return !clustermodule.Compare(oldCluster.Spec.ClusterModules, newCluster.Spec.ClusterModules)
			},
			CreateFunc:  func(e event.CreateEvent) bool { return false },
			DeleteFunc:  func(e event.DeleteEvent) bool { return false },
			GenericFunc: func(e event.GenericEvent) bool { return false },
		})
	if err != nil {
		return err
	}
	return nil
}

type vmReconciler struct {
	*context.ControllerContext
}

// Reconcile ensures the back-end state reflects the Kubernetes resource state intent.
func (r vmReconciler) Reconcile(ctx goctx.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	// Get the ICSVM resource for this request.
	icsVM := &infrav1.ICSVM{}
	if err := r.Client.Get(r, req.NamespacedName, icsVM); err != nil {
		if apierrors.IsNotFound(err) {
			r.Logger.Info("ICSVM not found, won't reconcile", "key", req.NamespacedName)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Create the patch helper.
	patchHelper, err := patch.NewHelper(icsVM, r.Client)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(
			err,
			"failed to init patch helper for %s %s/%s",
			icsVM.GroupVersionKind(),
			icsVM.Namespace,
			icsVM.Name)
	}

	// Fetch the owner ICSMachine.
	icsMachine, err := infrautilv1.GetOwnerICSMachine(r, r.Client, icsVM.ObjectMeta)
	// icsMachine can be nil in cases where custom mover other than clusterctl
	// moves the resources without ownerreferences set
	// in that case nil icsMachine can cause panic and CrashLoopBackOff the pod
	// preventing icsmachine_controller from setting the ownerref
	if err != nil || icsMachine == nil {
		r.Logger.Info("Owner ICSMachine not found, won't reconcile", "key", req.NamespacedName)
		return reconcile.Result{}, nil
	}

	icsCluster, err := infrautilv1.GetICSClusterFromICSMachine(r, r.Client, icsMachine)
	if err != nil || icsCluster == nil {
		r.Logger.Info("ICSCluster not found, won't reconcile", "key", ctrlclient.ObjectKeyFromObject(icsMachine))
		return reconcile.Result{}, nil
	}

	// Fetch the CAPI Machine.
	machine, err := clusterutilv1.GetOwnerMachine(r, r.Client, icsMachine.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if machine == nil {
		r.Logger.Info("Waiting for OwnerRef to be set on ICSMachine", "key", icsMachine.Name)
		return reconcile.Result{}, nil
	}

	clusterModule, err := r.fetchClusterModuleInfo(icsCluster, machine)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Create the VM context for this request.
	vmContext := &context.VMContext{
		ControllerContext: r.ControllerContext,
		ClusterModuleInfo: clusterModule,
		ICSVM:             icsVM,
		Session:           nil,
		Logger:            r.Logger.WithName(req.Namespace).WithName(req.Name),
		PatchHelper:       patchHelper,
	}

	// Print the task-ref upon entry and upon exit.
	vmContext.Logger.V(4).Info(
		"ICSVM.Status.TaskRef OnEntry",
		"task-ref", vmContext.ICSVM.Status.TaskRef)
	defer func() {
		vmContext.Logger.V(4).Info(
			"ICSVM.Status.TaskRef OnExit",
			"task-ref", vmContext.ICSVM.Status.TaskRef)
	}()

	// Always issue a patch when exiting this function so changes to the
	// resource are patched back to the API server.
	defer func() {
		// always update the readyCondition.
		conditions.SetSummary(vmContext.ICSVM,
			conditions.WithConditions(
				infrav1.VMProvisionedCondition,
				infrav1.ICenterAvailableCondition,
			),
		)

		// Patch the ICSVM resource.
		if err := vmContext.Patch(); err != nil {
			if reterr == nil {
				reterr = err
			}
			vmContext.Logger.Error(err, "patch failed", "vm", vmContext.String())
		}
	}()

	cluster, err := clusterutilv1.GetClusterFromMetadata(r.ControllerContext, r.Client, icsVM.ObjectMeta)
	if err == nil {
		if annotations.IsPaused(cluster, icsVM) {
			r.Logger.V(4).Info("ICSVM %s/%s linked to a cluster that is paused",
				icsVM.Namespace, icsVM.Name)
			return reconcile.Result{}, nil
		}
	}

	// Handle deleted machines
	if !icsVM.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(vmContext)
	}

	// Handle non-deleted machines
	return r.reconcileNormal(vmContext, icsMachine)
}

func (r vmReconciler) reconcileDelete(ctx *context.VMContext) (reconcile.Result, error) {
	ctx.Logger.Info("Handling deleted ICSVM")

	authSession, err := r.reconcileICenterConnectivity(ctx)
	if err == nil {
		conditions.MarkTrue(ctx.ICSVM, infrav1.ICenterAvailableCondition)
		ctx.Session = authSession

		// Implement selection of VM service based on ICS version
		var vmService services.VirtualMachineService = &basev1.VMService{}

		conditions.MarkFalse(ctx.ICSVM, infrav1.VMProvisionedCondition, clusterv1.DeletingReason, clusterv1.ConditionSeverityInfo, "")
		vm, err := vmService.DestroyVM(ctx)
		if err != nil {
			conditions.MarkFalse(ctx.ICSVM, infrav1.VMProvisionedCondition, "DeletionFailed", clusterv1.ConditionSeverityWarning, err.Error())
			return reconcile.Result{}, errors.Wrapf(err, "failed to destroy VM")
		}

		// Requeue the operation until the VM is "notfound".
		if vm.State != infrav1.VirtualMachineStateNotFound {
			ctx.Logger.Info("vm state is not reconciled", "expected-vm-state", infrav1.VirtualMachineStateNotFound, "actual-vm-state", vm.State)
			return reconcile.Result{}, nil
		}
	}

	// The VM is deleted so remove the finalizer.
	ctrlutil.RemoveFinalizer(ctx.ICSVM, infrav1.VMFinalizer)

	return reconcile.Result{}, nil
}

func (r vmReconciler) reconcileNormal(ctx *context.VMContext, icsMachine *infrav1.ICSMachine) (reconcile.Result, error) {
	if ctx.ICSVM.Status.FailureReason != nil || ctx.ICSVM.Status.FailureMessage != nil {
		r.Logger.Info("VM is failed, won't reconcile", "namespace", ctx.ICSVM.Namespace, "name", ctx.ICSVM.Name)
		return reconcile.Result{}, nil
	}
	// If the ICSVM doesn't have our finalizer, add it.
	ctrlutil.AddFinalizer(ctx.ICSVM, infrav1.VMFinalizer)

	if err := r.reconcileIdentitySecret(ctx); err != nil {
		conditions.MarkFalse(icsMachine, infrav1.ICenterAvailableCondition, infrav1.ICenterUnreachableReason, clusterv1.ConditionSeverityError, err.Error())
		return reconcile.Result{}, err
	}

	authSession, err := r.reconcileICenterConnectivity(ctx)
	if err != nil {
		conditions.MarkFalse(ctx.ICSVM, infrav1.ICenterAvailableCondition, infrav1.ICenterUnreachableReason, clusterv1.ConditionSeverityError, err.Error())
		return reconcile.Result{}, err
	}
	conditions.MarkTrue(ctx.ICSVM, infrav1.ICenterAvailableCondition)

	ctx.Session = authSession

	// Implement selection of VM service based on ICS version
	var vmService services.VirtualMachineService = &basev1.VMService{}

	if r.isWaitingForStaticIPAllocation(ctx) {
		conditions.MarkFalse(ctx.ICSVM, infrav1.VMProvisionedCondition, infrav1.WaitingForStaticIPAllocationReason, clusterv1.ConditionSeverityInfo, "")
		ctx.Logger.Info("vm is waiting for static ip to be available")
		return reconcile.Result{}, nil
	}

	// Get or create the VM.
	vm, err := vmService.ReconcileVM(ctx)
	if err != nil {
		ctx.Logger.Error(err, "error reconciling VM")
		return reconcile.Result{}, errors.Wrapf(err, "failed to reconcile VM")
	}

	// Do not proceed until the backend VM is marked ready.
	if vm.State != infrav1.VirtualMachineStateReady {
		ctx.Logger.Info(
			"VM state is not reconciled",
			"expected-vm-state", infrav1.VirtualMachineStateReady,
			"actual-vm-state", vm.State)
		return reconcile.Result{}, nil
	}

	// Update the ICSVM's BIOS UUID.
	ctx.Logger.Info("vm bios-uuid", "biosuuid", vm.BiosUUID)

	// defensive check to ensure we are not removing the biosUUID
	if vm.BiosUUID != "" {
		ctx.ICSVM.Spec.BiosUUID = vm.BiosUUID
	} else {
		return reconcile.Result{}, errors.Errorf("bios uuid is empty while VM is ready")
	}

	// Update the ICSVM's network status.
	r.reconcileNetwork(ctx, vm)

	// we didn't get any addresses, requeue
	if len(ctx.ICSVM.Status.Addresses) == 0 {
		return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Once the network is online the VM is considered ready.
	ctx.ICSVM.Status.Ready = true
	conditions.MarkTrue(ctx.ICSVM, infrav1.VMProvisionedCondition)
	ctx.Logger.Info("ICSVM is ready")

	return reconcile.Result{}, nil
}

// isWaitingForStaticIPAllocation checks whether the VM should wait for a static IP
// to be allocated.
// It checks the state of both DHCP4 and DHCP6 for all the network devices and if
// any static IP addresses are specified.
func (r vmReconciler) isWaitingForStaticIPAllocation(ctx *context.VMContext) bool {
	devices := ctx.ICSVM.Spec.Network.Devices
	for _, dev := range devices {
		if !dev.DHCP4 && !dev.DHCP6 && len(dev.IPAddrs) == 0 {
			// Static IP is not available yet
			return true
		}
	}

	return false
}

func (r vmReconciler) reconcileNetwork(ctx *context.VMContext, vm infrav1.VirtualMachine) {
	infrautilv1.UpdateNetworkInfo(ctx, vm.Network)
}

func (r vmReconciler) getClusterToICSVMsReq(a ctrlclient.Object) []reconcile.Request {
	requests := []reconcile.Request{}
	vms := &infrav1.ICSVMList{}
	err := r.Client.List(goctx.Background(), vms, ctrlclient.MatchingLabels(
		map[string]string{
			clusterv1.ClusterLabelName: a.GetName(),
		},
	))
	if err != nil {
		return requests
	}
	for _, vm := range vms.Items {
		r := reconcile.Request{
			NamespacedName: apitypes.NamespacedName{
				Name:      vm.Name,
				Namespace: vm.Namespace,
			},
		}
		requests = append(requests, r)
	}
	return requests
}

func (r vmReconciler) getICSClusterToICSVMsReq(a ctrlclient.Object) []reconcile.Request {
	icsCluster, ok := a.(*infrav1.ICSCluster)
	if !ok {
		return nil
	}
	clusterName, ok := icsCluster.Labels[clusterv1.ClusterLabelName]
	if !ok {
		return nil
	}

	requests := []reconcile.Request{}
	vms := &infrav1.ICSVMList{}
	err := r.Client.List(goctx.Background(), vms, ctrlclient.MatchingLabels(
		map[string]string{
			clusterv1.ClusterLabelName: clusterName,
		},
	))
	if err != nil {
		return requests
	}
	for _, vm := range vms.Items {
		r := reconcile.Request{
			NamespacedName: apitypes.NamespacedName{
				Name:      vm.Name,
				Namespace: vm.Namespace,
			},
		}
		requests = append(requests, r)
	}
	return requests
}

func (r vmReconciler) reconcileIdentitySecret(ctx *context.VMContext) error {
	icsVM := ctx.ICSVM
	if identity.IsMachineSecretIdentity(icsVM.Spec.IdentityRef) {
		secret := &corev1.Secret{}
		secretKey := ctrlclient.ObjectKey{
			Namespace: icsVM.Namespace,
			Name:      icsVM.Spec.IdentityRef.Name,
		}
		err := r.Client.Get(ctx, secretKey, secret)
		if err != nil {
			if infrautilv1.IsNotFoundError(err) {
				return nil
			}
			return err
		}
	}

	return nil
}

func (r vmReconciler) reconcileICenterConnectivity(ctx *context.VMContext) (*session.Session, error) {
	icsVM := ctx.ICSVM
	if err := identity.ValidateMachineInputs(r.Client, icsVM); err != nil {
		return nil, err
	}

	iCenter, err := identity.NewClientFromMachine(ctx, r.Client, icsVM.Namespace, icsVM.Spec.CloudName, icsVM.Spec.IdentityRef)
	if err != nil {
		if infrautilv1.IsNotFoundError(err) {
			sessionKey := ctx.ICSVM.Spec.CloudName
			return session.Get(ctx, sessionKey)
		}
		return nil, err
	}

	params := session.NewParams().
		WithCloudName(ctx.ICSVM.Spec.CloudName).
		WithServer(iCenter.ICenterURL).
		WithUserInfo(iCenter.AuthInfo.Username, iCenter.AuthInfo.Password).
		WithAPIVersion(iCenter.APIVersion).
		WithFeatures(session.Feature{
			KeepAliveDuration: r.KeepAliveDuration,
		})
	return session.GetOrCreate(ctx, params)
}

func (r vmReconciler) fetchClusterModuleInfo(icsCluster *infrav1.ICSCluster, machine *clusterv1.Machine) (*string, error) {
	var (
		owner ctrlclient.Object
		err   error
	)
	logger := r.Logger.WithName(machine.Namespace).WithName(machine.Name)

	input := infrautilv1.FetchObjectInput{
		Context: r.Context,
		Client:  r.Client,
		Object:  machine,
	}
	// Figure out a way to find the latest version of the CRD
	if infrautilv1.IsControlPlaneMachine(machine) {
		owner, err = infrautilv1.FetchControlPlaneOwnerObject(input)
	} else {
		owner, err = infrautilv1.FetchMachineDeploymentOwnerObject(input)
	}
	if err != nil {
		// If the owner objects cannot be traced, we can assume that the objects
		// have been deleted in which case we do not want cluster module info populated
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	for _, mod := range icsCluster.Spec.ClusterModules {
		if mod.TargetObjectName == owner.GetName() {
			logger.Info("cluster module with UUID found", "moduleUUID", mod.ModuleUUID)
			return pointer.String(mod.ModuleUUID), nil
		}
	}
	logger.V(4).Info("no cluster module found")
	return nil, nil
}
