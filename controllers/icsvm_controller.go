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
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	clusterutilv1 "sigs.k8s.io/cluster-api/util"
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

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1alpha3"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/record"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/services"
	infrast "github.com/inspur-ics/cluster-api-provider-ics/pkg/services/infrastructure"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/services/infrastructure/session"
	infrautilv1 "github.com/inspur-ics/cluster-api-provider-ics/pkg/util"
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=icsvms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=icsvms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch

// AddVMControllerToManager adds the VM controller to the provided manager.
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
		&handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(r.clusterToICSVMs),
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

type vmReconciler struct {
	*context.ControllerContext
}

// Reconcile ensures the back-end state reflects the Kubernetes resource state intent.
// nolint:gocognit
func (r vmReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, reterr error) {
	// Get the ICSVM resource for this request.
	icsVM := &infrav1.ICSVM{}
	if err := r.Client.Get(r, req.NamespacedName, icsVM); err != nil {
		if apierrors.IsNotFound(err) {
			r.Logger.Info("ICSVM not found, won't reconcile", "key", req.NamespacedName)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Get or create an authenticated session to the ics endpoint.
	authSession, err := session.GetOrCreate(r.Context,
		icsVM.Spec.Server, icsVM.Spec.Datacenter,
		r.ControllerManagerContext.Username, r.ControllerManagerContext.Password)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to create ics session")
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

	// Create the VM context for this request.
	vmContext := &context.VMContext{
		ControllerContext: r.ControllerContext,
		ICSVM:             icsVM,
		Session:           authSession,
		Logger:            r.Logger.WithName(req.Namespace).WithName(req.Name),
		PatchHelper:       patchHelper,
	}

	template, err := r.reconcileTemplate(vmContext)
	if err != nil {
		return reconcile.Result{}, err
	}
	vmContext.Template = template

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
		// Patch the ICSVM resource.
		if err := vmContext.Patch(); err != nil {
			if reterr == nil {
				reterr = err
			}
			vmContext.Logger.Error(err, "patch failed", "vm", vmContext.String())
		}

		// localObj is a deep copy of the ICSVM resource that was
		// fetched at the top of this Reconcile function.
		localObj := vmContext.ICSVM.DeepCopy()

		// Fetch the up-to-date ICSVM resource into remoteObj until the
		// fetched resource has a a different ResourceVersion than the local
		// object.
		//
		// FYI - resource versions are opaque, numeric strings and should not
		// be compared with < or >, only for equality -
		// https://kubernetes.io/docs/reference/using-api/api-concepts/#resource-versions.
		//
		// Since CAPICS is currently deployed with a single replica, and this
		// controller has a max concurrency of one, the only agent updating the
		// ICSVM resource should be this controller.
		//
		// So if the remote resource's ResourceVersion is different than the
		// ResourceVersion of the resource fetched at the beginning of this
		// reconcile request, then that means the remote resource should be
		// newer than the local resource.
		// nolint:errcheck
		wait.PollImmediateInfinite(time.Second*1, func() (bool, error) {
			// remoteObj refererences the same ICSVM resource as it exists
			// on the API server post the patch operation above. In a perfect world,
			// the Status for localObj and remoteObj should be the same.
			remoteObj := &infrav1.ICSVM{}
			if err := vmContext.Client.Get(vmContext, req.NamespacedName, remoteObj); err != nil {
				if apierrors.IsNotFound(err) {
					// It's possible that the remote resource cannot be found
					// because it has been removed. Do not error, just exit.
					return true, nil
				}

				// There was an issue getting the remote resource. Sleep for a
				// second and try again.
				vmContext.Logger.Error(err, "failed to get ICSVM while exiting reconcile")
				return false, nil
			}
			// If the remote resource version is not the same as the local
			// resource version, then it means we were able to get a resource
			// newer than the one we already had.
			if localObj.ResourceVersion != remoteObj.ResourceVersion {
				vmContext.Logger.Info(
					"resource is patched",
					"local-resource-version", localObj.ResourceVersion,
					"remote-resource-version", remoteObj.ResourceVersion)
				return true, nil
			}

			// If the resources are the same resource version, then a previous
			// patch may not have resulted in any changes. Check to see if the
			// remote status is the same as the local status.
			if cmp.Equal(localObj.Status, remoteObj.Status, cmpopts.EquateEmpty()) {
				vmContext.Logger.Info(
					"resource patch was not required",
					"local-resource-version", localObj.ResourceVersion,
					"remote-resource-version", remoteObj.ResourceVersion)
				return true, nil
			}

			// The remote resource version is the same as the local resource
			// version, which means the local cache is not yet up-to-date.
			vmContext.Logger.Info(
				"resource is not patched",
				"local-resource-version", localObj.ResourceVersion,
				"remote-resource-version", remoteObj.ResourceVersion)
			return false, nil
		})
	}()

	cluster, err := clusterutilv1.GetClusterFromMetadata(r.ControllerContext, r.Client, icsVM.ObjectMeta)
	if err == nil {
		if clusterutilv1.IsPaused(cluster, icsVM) {
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
	return r.reconcileNormal(vmContext)
}

func (r vmReconciler) reconcileDelete(ctx *context.VMContext) (reconcile.Result, error) {
	ctx.Logger.Info("Handling deleted ICSVM")

	//Implement selection of VM service based on ics version
	var vmService services.VirtualMachineService = &infrast.VMService{}

	vm, err := vmService.DestroyVM(ctx)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to destroy VM")
	}

	// Requeue the operation until the VM is "notfound".
	if vm.State != infrav1.VirtualMachineStateNotFound {
		ctx.Logger.Info("vm state is not reconciled", "expected-vm-state", infrav1.VirtualMachineStateNotFound, "actual-vm-state", vm.State)
		return reconcile.Result{}, nil
	}

	// The VM is deleted so remove the finalizer.
	ctrlutil.RemoveFinalizer(ctx.ICSVM, infrav1.VMFinalizer)

	return reconcile.Result{}, nil
}

func (r vmReconciler) reconcileNormal(ctx *context.VMContext) (reconcile.Result, error) {

	if ctx.ICSVM.Status.FailureReason != nil || ctx.ICSVM.Status.FailureMessage != nil {
		r.Logger.Info("VM is failed, won't reconcile", "namespace", ctx.ICSVM.Namespace, "name", ctx.ICSVM.Name)
		return reconcile.Result{}, nil
	}
	// If the ICSVM doesn't have our finalizer, add it.
	ctrlutil.AddFinalizer(ctx.ICSVM, infrav1.VMFinalizer)

	//Implement selection of VM service based on ics version
	var vmService services.VirtualMachineService = &infrast.VMService{}

	// Get or create the VM.
	vm, err := vmService.ReconcileVM(ctx)
	if err != nil {
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

	// Once the network is online the VM is considered ready.
	ctx.ICSVM.Status.Ready = true
	ctx.Logger.Info("ICSVM is ready")

	return reconcile.Result{}, nil
}

func (r vmReconciler) reconcileNetwork(ctx *context.VMContext, vm infrav1.VirtualMachine) {
	ctx.ICSVM.Status.Network = vm.Network
	ipAddrs := make([]string, 0, len(vm.Network))
	for _, netStatus := range ctx.ICSVM.Status.Network {
		ipAddrs = append(ipAddrs, netStatus.IPAddrs...)
	}
	ctx.ICSVM.Status.Addresses = ipAddrs
}

func (r vmReconciler) reconcileTemplate(ctx *context.VMContext) (*infrav1.ICSMachineTemplate, error) {
	// Get ICSMachine for ICSVM
	icsMachine, err := infrautilv1.GetOwnerICSMachine(r, r.Client, ctx.ICSVM.ObjectMeta)
	if err != nil {
		return nil, errors.Wrapf(err,
			"failed to get ICSMachine for the ICSVM %s", ctx.ICSVM.Name)
	}

	// Get ICSMachineTemplate Info for VirtualMachine
	icsMachineTemplate := &infrav1.ICSMachineTemplate{}
	machineTemplateType := icsMachine.ObjectMeta.Annotations[infrav1.TemplateClonedFromGroupKindAnnotation]
	if machineTemplateType == icsMachineTemplate.GroupVersionKind().GroupKind().String() {
		templateName := icsMachine.ObjectMeta.Annotations[infrav1.TemplateClonedFromNameAnnotation]
		namespacedName := apitypes.NamespacedName{
			Namespace: ctx.ICSVM.Namespace,
			Name:      templateName,
		}
		if err := r.Client.Get(r, namespacedName, icsMachineTemplate); err != nil {
			return nil, err
		}
		template := icsMachineTemplate.DeepCopy()
		if template.Spec.Ipam == nil {
			template.Spec.Ipam = &infrav1.IPAMSpec{
				Cluster: infrautilv1.GetOwnerClusterName(template.ObjectMeta),
			}
			devices := template.Spec.Template.Spec.Network.Devices
			var pools  []infrav1.Pool
			var status []infrav1.IPPoolStatus
			for _, device := range devices {
				stat := &infrav1.IPPoolStatus{
					NetworkName: device.NetworkName,
				}
				status = append(status, *stat)
				pool := &infrav1.Pool{
					NetworkName: device.NetworkName,
				}
				if !device.DHCP4 && !device.DHCP6 {
					pool.Static = true
					pool.IPAddrs = device.IPAddrs
					pool.PreAllocations =infrautilv1.ConvertIPAddrsToPreAllocations(device.IPAddrs)
				} else {
					pool.Static = false
				}
				pools = append(pools, *pool)
			}
			now := metav1.Now()
			template.Status = infrav1.ICSMachineTemplateStatus{
				LastUpdated:   &now,
				Pools:         status,
			}
			if err := r.Client.Update(ctx, template); err != nil {
				return icsMachineTemplate, errors.Wrapf(err,
					"error changing ICSMachineTemplate %s/%s IPAMSpec",
					icsMachineTemplate.GetNamespace(), icsMachineTemplate.GetName())
			}
			icsMachineTemplate = template
		}
	}
	return icsMachineTemplate, nil
}

func (r *vmReconciler) clusterToICSVMs(a handler.MapObject) []reconcile.Request {
	requests := []reconcile.Request{}
	vms := &infrav1.ICSVMList{}
	err := r.Client.List(goctx.Background(), vms, ctrlclient.MatchingLabels(
		map[string]string{
			clusterv1.ClusterLabelName: a.Meta.GetName(),
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
