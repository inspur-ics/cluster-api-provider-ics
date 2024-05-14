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

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apitypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
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
	r := ipAddressReconciler{ControllerContext: controllerContext}
	_, err := ctrl.NewControllerManagedBy(mgr).
		// Watch the controlled, infrastructure resource.
		For(controlledType).
		// Watch any IPAddress resources owned by the controlled type.
		Watches(
			&source.Kind{Type: &infrav1.ICSVM{}},
			handler.EnqueueRequestsFromMapFunc(r.getVMToIPAddressesReq),
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
	return nil
}

type ipAddressReconciler struct {
	*context.ControllerContext
}

// Reconcile ensures the back-end state reflects the Kubernetes resource state intent.
func (r ipAddressReconciler) Reconcile(ctx goctx.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	// Get the IPAddress resource for this request.
	ipAddress := &infrav1.IPAddress{}
	if err := r.Client.Get(r, req.NamespacedName, ipAddress); err != nil {
		if apierrors.IsNotFound(err) {
			r.Logger.Info("IPAddress not found, won't reconcile", "key", req.NamespacedName)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
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

func (r ipAddressReconciler) reconcileDelete(ctx *context.IPAddressContext) (reconcile.Result, error) {
	ctx.Logger.Info("Handling deleted IPAddress")

	// The IPAddress is deleted so remove the finalizer.
	ctrlutil.RemoveFinalizer(ctx.IPAddress, infrav1.IPAddressFinalizer)
	return reconcile.Result{}, nil
}

func (r ipAddressReconciler) reconcileNormal(ctx *context.IPAddressContext) (reconcile.Result, error) {// If the ICSMachine doesn't have our finalizer, add it.
	ctrlutil.AddFinalizer(ctx.IPAddress, infrav1.IPAddressFinalizer)
	return reconcile.Result{}, nil
}

// vmToIPAddresses is a handler.ToRequestsFunc to be used
// to enqueue requests for reconciliation for IPAddress to update
// its status.apiEndpoints field.
func (r ipAddressReconciler) getVMToIPAddressesReq(a ctrlclient.Object) []reconcile.Request {
	requests := []reconcile.Request{}
	ipAddresses := &infrav1.IPAddressList{}
	err := r.Client.List(goctx.Background(), ipAddresses,
		ctrlclient.InNamespace(a.GetNamespace()),
		ctrlclient.MatchingFields{"spec.vmRef.name": a.GetName()},
	)
	if err != nil {
		return requests
	}
	for _, ipAddress := range ipAddresses.Items {
		if !ipAddress.ObjectMeta.DeletionTimestamp.IsZero() {
			continue
		}
		r := reconcile.Request{
			NamespacedName: apitypes.NamespacedName{
				Name:      ipAddress.Name,
				Namespace: ipAddress.Namespace,
			},
		}
		requests = append(requests, r)
	}
	return requests
}