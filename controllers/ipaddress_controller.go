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

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apitypes "k8s.io/apimachinery/pkg/types"
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
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=ipaddresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=icsvms;icsvms/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch

// AddIPAddressControllerToManager adds the machine controller to the provided
// manager.
func AddIPAddressControllerToManager(ctx *context.ControllerManagerContext, mgr manager.Manager) error {

	var (
		controlledType     = &infrav1.IPAddress{}
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

	r := ipaddressReconciler{ControllerContext: controllerContext}

	controller, err := ctrl.NewControllerManagedBy(mgr).
		// Watch the controlled, infrastructure resource.
		For(controlledType).
		// Watch any IPAddress resources owned by the controlled type.
		Watches(
			&source.Kind{Type: &infrav1.ICSVM{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.vmToIPAddresses),
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
			ToRequests: handler.ToRequestsFunc(r.clusterToIPAddresses),
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

type ipaddressReconciler struct {
	*context.ControllerContext
}

// Reconcile ensures the back-end state reflects the Kubernetes resource state intent.
func (r ipaddressReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, reterr error) {

	// Get the IPAddress resource for this request.
	ipAddress := &infrav1.IPAddress{}
	if err := r.Client.Get(r, req.NamespacedName, ipAddress); err != nil {
		if apierrors.IsNotFound(err) {
			r.Logger.Info("IPAddress not found, won't reconcile", "key", req.NamespacedName)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Fetch the ICSVM.
	icsvm, err := r.getICSVMByIPAddress(r.ControllerContext, ipAddress)
	if err != nil {
		return reconcile.Result{}, err
	}
	if icsvm == nil {
		r.Logger.Info("Waiting for ICSVM Controller to set vmRef on IPAddress")
		return reconcile.Result{}, nil
	}

	// Fetch the CAPI Cluster.
	cluster, err := clusterutilv1.GetClusterFromMetadata(r, r.Client, icsvm.ObjectMeta)
	if err != nil {
		r.Logger.Info("ICSVM is missing cluster label or cluster does not exist")
		return reconcile.Result{}, nil
	}
	if clusterutilv1.IsPaused(cluster, icsvm) {
		r.Logger.V(4).Info("ICSVM %s/%s linked to a cluster that is paused",
			icsvm.Namespace, icsvm.Name)
		return reconcile.Result{}, nil
	}

	// Fetch the ICSCluster
	icsCluster := &infrav1.ICSCluster{}
	icsClusterName := ctrlclient.ObjectKey{
		Namespace: ipAddress.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	if err := r.Client.Get(r, icsClusterName, icsCluster); err != nil {
		r.Logger.Info("Waiting for ICSCluster")
		return reconcile.Result{}, nil
	}

	// Create the patch helper.
	patchHelper, err := patch.NewHelper(ipAddress, r.Client)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(
			err,
			"failed to init patch helper for %s %s/%s",
			ipAddress.GroupVersionKind(),
			ipAddress.Namespace,
			ipAddress.Name)
	}

	// Create the ipAddress context for this request.
	ipAddressContext := &context.IPAddressContext{
		ControllerContext: r.ControllerContext,
		Cluster:           cluster,
		ICSCluster:        icsCluster,
		ICSVM:             icsvm,
		IPAddress:         ipAddress,
		Logger:            r.Logger.WithName(req.Namespace).WithName(req.Name),
		PatchHelper:       patchHelper,
	}

	// Always issue a patch when exiting this function so changes to the
	// resource are patched back to the API server.
	defer func() {
		// Patch the ICSMachine resource.
		if err := ipAddressContext.Patch(); err != nil {
			if reterr == nil {
				reterr = err
			}
			ipAddressContext.Logger.Error(err, "patch failed", "ipaddress", ipAddressContext.String())
		}
	}()

	// Handle deleted machines
	if !ipAddress.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ipAddressContext)
	}

	// Handle non-deleted machines
	return r.reconcileNormal(ipAddressContext)
}

func (r ipaddressReconciler) getICSVMByIPAddress(ctx *context.ControllerContext, ipAddress *infrav1.IPAddress) (*infrav1.ICSVM, error) {
	// Get ready to find the associated ICSVM resource.
	vm := &infrav1.ICSVM{}
	vmKey := apitypes.NamespacedName{
		Namespace: ipAddress.Namespace,
		Name:      ipAddress.Spec.VMRef.Name,
	}
	// Attempt to find the associated ICSVM resource.
	if err := ctx.Client.Get(ctx, vmKey, vm); err != nil {
		return nil, err
	}
	return vm, nil
}

func (r ipaddressReconciler) reconcileDelete(ctx *context.IPAddressContext) (reconcile.Result, error) {
	ctx.Logger.Info("Handling deleted IPAddress")

	// The IPAddress is deleted so remove the finalizer.
	ctrlutil.RemoveFinalizer(ctx.IPAddress, infrav1.IPAddressFinalizer)
	return reconcile.Result{}, nil
}

func (r ipaddressReconciler) reconcileNormal(ctx *context.IPAddressContext) (reconcile.Result, error) {// If the ICSMachine doesn't have our finalizer, add it.
	ctrlutil.AddFinalizer(ctx.IPAddress, infrav1.IPAddressFinalizer)

	if !ctx.Cluster.Status.InfrastructureReady {
		ctx.Logger.Info("Cluster infrastructure is not ready yet")
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}

func (r *ipaddressReconciler) clusterToIPAddresses(a handler.MapObject) []reconcile.Request {
	requests := []reconcile.Request{}
	ipaddresses := &infrav1.IPAddressList{}
	err := r.Client.List(goctx.Background(), ipaddresses,
		ctrlclient.InNamespace(a.Meta.GetNamespace()),
		ctrlclient.MatchingLabels(
		map[string]string{
			clusterv1.ClusterLabelName: a.Meta.GetName(),
		},
	))
	if err != nil {
		return requests
	}
	for _, ipaddress := range ipaddresses.Items {
		r := reconcile.Request{
			NamespacedName: apitypes.NamespacedName{
				Name:      ipaddress.Name,
				Namespace: ipaddress.Namespace,
			},
		}
		requests = append(requests, r)
	}
	return requests
}

// vmToIPAddresses is a handler.ToRequestsFunc to be used
// to enqueue requests for reconciliation for IPAddress to update
// its status.apiEndpoints field.
func (r ipaddressReconciler) vmToIPAddresses(o handler.MapObject) []ctrl.Request {
	requests := []reconcile.Request{}
	ipaddresses := &infrav1.IPAddressList{}
	err := r.Client.List(goctx.Background(), ipaddresses,
		ctrlclient.InNamespace(o.Meta.GetNamespace()),
		ctrlclient.MatchingFields{"spec.vmRef.name": o.Meta.GetName()},
	)
	if err != nil {
		return requests
	}
	for _, ipaddress := range ipaddresses.Items {
		if !ipaddress.ObjectMeta.DeletionTimestamp.IsZero() {
			continue
		}
		r := reconcile.Request{
			NamespacedName: apitypes.NamespacedName{
				Name:      ipaddress.Name,
				Namespace: ipaddress.Namespace,
			},
		}
		requests = append(requests, r)
	}
	return requests
}