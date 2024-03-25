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
	"sync"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	clusterutilv1 "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/identity"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/record"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/session"
	infrautilv1 "github.com/inspur-ics/cluster-api-provider-ics/pkg/util"
)

var (
	defaultAPIEndpointPort = int32(6443)
)

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;patch;update
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=icsclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=icsclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch;update;patch

// AddClusterControllerToManager adds the cluster controller to the provided
// manager.
func AddClusterControllerToManager(ctx *context.ControllerManagerContext, mgr manager.Manager, clusterControlledType client.Object) error {
	var (
		clusterControlledTypeName = reflect.TypeOf(clusterControlledType).Elem().Name()
		clusterControlledTypeGVK  = infrav1.GroupVersion.WithKind(clusterControlledTypeName)
		controllerNameShort       = fmt.Sprintf("%s-controller", strings.ToLower(clusterControlledTypeName))
		controllerNameLong        = fmt.Sprintf("%s/%s/%s", ctx.Namespace, ctx.Name, controllerNameShort)
	)

	// Build the controller context.
	controllerContext := &context.ControllerContext{
		ControllerManagerContext: ctx,
		Name:                     controllerNameShort,
		Recorder:                 record.New(mgr.GetEventRecorderFor(controllerNameLong)),
		Logger:                   ctx.Logger.WithName(controllerNameShort),
	}

	reconciler := clusterReconciler{
		ControllerContext:       controllerContext,
	}
	clusterToInfraFn := clusterutilv1.ClusterToInfrastructureMapFunc(clusterControlledTypeGVK)
	_, err := ctrl.NewControllerManagedBy(mgr).
		// Watch the controlled, infrastructure resource.
		For(clusterControlledType).
		// Watch the CAPI resource that owns this infrastructure resource.
		Watches(
			&source.Kind{Type: &clusterv1.Cluster{}},
			handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
				requests := clusterToInfraFn(o)
				if requests == nil {
					return nil
				}

				c := &infrav1.ICSCluster{}
				if err := reconciler.Client.Get(ctx, requests[0].NamespacedName, c); err != nil {
					reconciler.Logger.V(4).Error(err, "Failed to get ICSCluster")
					return nil
				}

				if annotations.IsExternallyManaged(c) {
					reconciler.Logger.V(4).Info("ICSCluster is externally managed, skipping mapping.")
					return nil
				}
				return requests
			}),
		).

		// Watch the infrastructure machine resources that belong to the control
		// plane. This controller needs to reconcile the infrastructure cluster
		// once a control plane machine has an IP address.
		Watches(
			&source.Kind{Type: &infrav1.ICSMachine{}},
			handler.EnqueueRequestsFromMapFunc(reconciler.getControlPlaneMachineToClusterReq),
		).
		// Watch a GenericEvent channel for the controlled resource.
		//
		// This is useful when there are events outside of Kubernetes that
		// should cause a resource to be synchronized, such as a goroutine
		// waiting on some asynchronous, external task to complete.
		Watches(
			&source.Channel{Source: ctx.GetGenericEventChannelFor(clusterControlledTypeGVK)},
			&handler.EnqueueRequestForObject{},
		).
		WithEventFilter(predicates.ResourceIsNotExternallyManaged(reconciler.Logger)).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctx.MaxConcurrentReconciles}).
		Build(reconciler)
	if err != nil {
		return err
	}

	return nil
}

type clusterReconciler struct {
	*context.ControllerContext
}

// Reconcile ensures the back-end state reflects the Kubernetes resource state intent.
func (r clusterReconciler) Reconcile(ctx goctx.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	// Get the ICSCluster resource for this request.
	icsCluster := &infrav1.ICSCluster{}
	if err := r.Client.Get(r, req.NamespacedName, icsCluster); err != nil {
		if apierrors.IsNotFound(err) {
			r.Logger.V(4).Info("ICSCluster not found, won't reconcile", "key", req.NamespacedName)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Fetch the CAPI Cluster.
	cluster, err := clusterutilv1.GetOwnerCluster(r, r.Client, icsCluster.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if cluster == nil {
		r.Logger.Info("Waiting for Cluster Controller to set OwnerRef on ICSCluster")
		return reconcile.Result{}, nil
	}
	if annotations.IsPaused(cluster, icsCluster) {
		r.Logger.V(4).Info("ICSCluster %s/%s linked to a cluster that is paused",
			icsCluster.Namespace, icsCluster.Name)
		return reconcile.Result{}, nil
	}

	// Create the patch helper.
	patchHelper, err := patch.NewHelper(icsCluster, r.Client)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(
			err,
			"failed to init patch helper for %s %s/%s",
			icsCluster.GroupVersionKind(),
			icsCluster.Namespace,
			icsCluster.Name)
	}

	// Create the cluster context for this request.
	clusterContext := &context.ClusterContext{
		ControllerContext: r.ControllerContext,
		Cluster:           cluster,
		ICSCluster:        icsCluster,
		Logger:            r.Logger.WithName(req.Namespace).WithName(req.Name),
		PatchHelper:       patchHelper,
	}

	// Always issue a patch when exiting this function so changes to the
	// resource are patched back to the API server.
	defer func() {
		if err := clusterContext.Patch(); err != nil {
			if reterr == nil {
				reterr = err
			}
			clusterContext.Logger.Error(err, "patch failed", "cluster", clusterContext.String())
		}
	}()

	if err := setOwnerRefsOnICSMachines(clusterContext); err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to set owner refs on ICSMachine objects")
	}

	// Handle deleted clusters
	if !icsCluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(clusterContext)
	}

	// Handle non-deleted clusters
	return r.reconcileNormal(clusterContext)
}

func (r clusterReconciler) reconcileDelete(ctx *context.ClusterContext) (reconcile.Result, error) {
	ctx.Logger.Info("Reconciling ICSCluster delete")

	icsMachines, err := infrautilv1.GetICSMachinesInCluster(ctx, ctx.Client, ctx.Cluster.Namespace, ctx.Cluster.Name)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err,
			"unable to list ICSMachines part of ICSCluster %s/%s", ctx.ICSCluster.Namespace, ctx.ICSCluster.Name)
	}

	machineDeletionCount := 0
	deletionErrors := []error{}
	for _, icsMachine := range icsMachines {
		// If the ICSMachine is not owned by the CAPI Machine object because the machine object was deleted
		// before setting the owner references, then proceed with the deletion of the ICSMachine object.
		if clusterutilv1.IsOwnedByObject(icsMachine, ctx.ICSCluster) && len(icsMachine.OwnerReferences) == 1 {
			machineDeletionCount++
			// Remove the finalizer since VM creation wouldn't proceed
			r.Logger.Info("Removing finalizer from ICSMachine", "namespace", icsMachine.Namespace, "name", icsMachine.Name)
			ctrlutil.RemoveFinalizer(icsMachine, infrav1.MachineFinalizer)
			if err := r.Client.Update(ctx, icsMachine); err != nil {
				return reconcile.Result{}, err
			}
			if err := r.Client.Delete(ctx, icsMachine); err != nil && !apierrors.IsNotFound(err) {
				ctx.Logger.Error(err, "Failed to delete for ICSMachine", "namespace", icsMachine.Namespace, "name", icsMachine.Name)
				deletionErrors = append(deletionErrors, err)
			}
		}
	}
	if len(deletionErrors) > 0 {
		return reconcile.Result{}, kerrors.NewAggregate(deletionErrors)
	}

	if len(icsMachines)-machineDeletionCount > 0 {
		ctx.Logger.Info("Waiting for ICSMachines to be deleted", "count", len(icsMachines)-machineDeletionCount)
		return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Cluster is deleted so remove the finalizer.
	ctrlutil.RemoveFinalizer(ctx.ICSCluster, infrav1.ClusterFinalizer)

	return reconcile.Result{}, nil
}

func (r clusterReconciler) reconcileNormal(ctx *context.ClusterContext) (reconcile.Result, error) {
	ctx.Logger.Info("Reconciling ICSCluster")

	// If the ICSCluster doesn't have our finalizer, add it.
	ctrlutil.AddFinalizer(ctx.ICSCluster, infrav1.ClusterFinalizer)

	if err := r.reconcileIdentitySecret(ctx); err != nil {
		conditions.MarkFalse(ctx.ICSCluster, infrav1.ICenterAvailableCondition, infrav1.ICenterUnreachableReason, clusterv1.ConditionSeverityError, err.Error())
		return reconcile.Result{}, err
	}

	iCenterSession, err := r.reconcileICenterConnectivity(ctx)
	if err != nil {
		conditions.MarkFalse(ctx.ICSCluster, infrav1.ICenterAvailableCondition, infrav1.ICenterUnreachableReason, clusterv1.ConditionSeverityError, err.Error())
		return reconcile.Result{}, errors.Wrapf(err,
			"unexpected error while probing icenter for %s", ctx)
	}
	conditions.MarkTrue(ctx.ICSCluster, infrav1.ICenterAvailableCondition)

	err = r.reconcileICenterVersion(ctx, iCenterSession)
	if err != nil || ctx.ICSCluster.Status.ICenterVersion == "" {
		conditions.MarkFalse(ctx.ICSCluster, infrav1.ClusterModulesAvailableCondition, infrav1.MissingICenterVersionReason, clusterv1.ConditionSeverityWarning, "iCenter API version not set")
		ctx.Logger.Error(err, "could not reconcile iCenter version")
	}

	// Reconcile the ICSCluster's load balancer.
	if ok, err := r.reconcileLoadBalancer(ctx); !ok {
		if err != nil {
			return reconcile.Result{}, errors.Wrapf(err,
				"unexpected error while reconciling load balancer for %s", ctx)
		}
		ctx.Logger.Info("load balancer is not reconciled")
		return reconcile.Result{}, nil
	}

	ctx.ICSCluster.Status.Ready = true

	// Reconcile the ICSCluster resource's control plane endpoint.
	if ok, err := r.reconcileControlPlaneEndpoint(ctx); !ok {
		if err != nil {
			return reconcile.Result{}, errors.Wrapf(err,
				"unexpected error while reconciling control plane endpoint for %s", ctx)
		}
		ctx.Logger.Info("control plane endpoint is not reconciled")
		return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Ensure the ICSCluster is reconciled when the API server first comes online.
	// A reconcile event will only be triggered if the Cluster is not marked as
	// ControlPlaneInitialized.
	r.reconcileICSClusterWhenAPIServerIsOnline(ctx)
	if ctx.ICSCluster.Spec.ControlPlaneEndpoint.IsZero() {
		ctx.Logger.Info("control plane endpoint is not reconciled")
		return reconcile.Result{}, nil
	}

	// If the cluster is deleted, that's mean that the workload cluster is being deleted and so the CCM/CSI instances
	if !ctx.Cluster.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, nil
	}

	// Wait until the API server is online and accessible.
	if !r.isAPIServerOnline(ctx) {
		return reconcile.Result{}, nil
	}

	// Sync Custom kubeconfig
	r.syncCustomKubeConfig(ctx)

	return reconcile.Result{}, nil
}

func (r clusterReconciler) reconcileIdentitySecret(ctx *context.ClusterContext) error {
	icsCluster := ctx.ICSCluster
	if identity.IsSecretIdentity(icsCluster) {
		secret := &corev1.Secret{}
		secretKey := client.ObjectKey{
			Namespace: icsCluster.Namespace,
			Name:      icsCluster.Spec.IdentityRef.Name,
		}
		err := ctx.Client.Get(ctx, secretKey, secret)
		if err != nil {
			if infrautilv1.IsNotFoundError(err) {
				return nil
			}
			return err
		}
	}

	return nil
}

func (r clusterReconciler) reconcileICenterConnectivity(ctx *context.ClusterContext) (*session.Session, error) {
	if err := identity.ValidateInputs(r.Client, ctx.ICSCluster); err != nil {
		return nil, err
	}

	iCenter, err := identity.NewClientFromCluster(ctx, r.Client, ctx.ICSCluster)
	if err != nil {
		if infrautilv1.IsNotFoundError(err) {
			sessionKey := ctx.ICSCluster.Spec.CloudName
			return session.Get(ctx, sessionKey)
		}
		return nil, err
	}

	params := session.NewParams().
		WithCloudName(ctx.ICSCluster.Spec.CloudName).
		WithServer(iCenter.ICenterURL).
		WithUserInfo(iCenter.AuthInfo.Username, iCenter.AuthInfo.Password).
		WithAPIVersion(iCenter.APIVersion).
		WithFeatures(session.Feature{
			KeepAliveDuration: r.KeepAliveDuration,
		})
	return session.GetOrCreate(ctx, params)
}

func (r clusterReconciler) reconcileICenterVersion(ctx *context.ClusterContext, s *session.Session) error {
	version, err := s.GetVersion()
	if err != nil {
		return err
	}
	ctx.ICSCluster.Status.ICenterVersion = version
	return nil
}

func (r clusterReconciler) reconcileLoadBalancer(ctx *context.ClusterContext) (bool, error) {
	if !ctx.Cluster.Spec.ControlPlaneEndpoint.IsZero() {
		ctx.ICSCluster.Spec.ControlPlaneEndpoint.Host = ctx.Cluster.Spec.ControlPlaneEndpoint.Host
		ctx.ICSCluster.Spec.ControlPlaneEndpoint.Port = ctx.Cluster.Spec.ControlPlaneEndpoint.Port
	}

	if ctx.ICSCluster.Spec.ControlPlaneEndpoint.IsZero() {
		ctx.ICSCluster.Spec.ControlPlaneEndpoint.Host = "127.0.0.1"
		ctx.ICSCluster.Spec.EnabledLoadBalancer = false
	} else if ctx.ICSCluster.Spec.ControlPlaneEndpoint.Host != "127.0.0.1" {
		ctx.ICSCluster.Spec.EnabledLoadBalancer = true
	}

	// Update the ICSCluster.Spec.ControlPlaneEndpoint with the address
	// from the load balancer.
	// The control plane endpoint also requires a port, which is obtained
	// either from the ICSCluster.Spec.ControlPlaneEndpoint.Port
	// or the default port is used.
	if ctx.ICSCluster.Spec.ControlPlaneEndpoint.Port == 0 {
		ctx.ICSCluster.Spec.ControlPlaneEndpoint.Port = defaultAPIEndpointPort
	}
	ctx.Logger.Info("ControlPlaneEndpoint discovered via load balancer",
		"controlPlaneEndpoint", ctx.ICSCluster.Spec.ControlPlaneEndpoint.String())

	return true, nil
}


func (r clusterReconciler) reconcileControlPlaneEndpoint(ctx *context.ClusterContext) (bool, error) {
	ctx.Logger.Info("Reconciling control plane endpoint")
	if ctx.ICSCluster.Spec.EnabledLoadBalancer {
		if !ctx.Cluster.Spec.ControlPlaneEndpoint.IsZero() {
			ctx.ICSCluster.Spec.ControlPlaneEndpoint.Host = ctx.Cluster.Spec.ControlPlaneEndpoint.Host
			ctx.ICSCluster.Spec.ControlPlaneEndpoint.Port = ctx.Cluster.Spec.ControlPlaneEndpoint.Port
			conditions.MarkTrue(ctx.ICSCluster, infrav1.LoadBalancerReadyCondition)
			ctx.Logger.Info("skipping control plane endpoint reconciliation",
				"reason", "ControlPlaneEndpoint already set on Cluster",
				"controlPlaneEndpoint", ctx.Cluster.Spec.ControlPlaneEndpoint.String())
			return true, nil
		}

		if !ctx.ICSCluster.Spec.ControlPlaneEndpoint.IsZero() {
			conditions.MarkTrue(ctx.ICSCluster, infrav1.LoadBalancerReadyCondition)
			ctx.Logger.Info("skipping control plane endpoint reconciliation",
				"reason", "ControlPlaneEndpoint already set on icsCluster",
				"controlPlaneEndpoint", ctx.ICSCluster.Spec.ControlPlaneEndpoint.String())
			return true, nil
		}

		return false, errors.Wrapf(errors.New("no load-balanced control plane endpoint"),
			"failed to reconcile loadbalanced endpoint for icsCluster %s/%s",
			ctx.ICSCluster.Namespace, ctx.ICSCluster.Name)
	}

	icsMachines, err := infrautilv1.GetControlPlaneICSMachinesInCluster(ctx, ctx.Client, ctx.Cluster.Namespace, ctx.Cluster.Name)
	//machines, err := collections.GetFilteredMachinesForCluster(ctx, r.Client, ctx.Cluster, collections.ControlPlaneMachines(ctx.Cluster.Name))
	if err != nil {
		if infrautilv1.IsNotFoundError(err) || len(icsMachines) == 0 {
			return false, nil
		}
		return false, errors.Wrapf(err,
			"failed to get ICSMachines for Cluster %s/%s",
			ctx.ICSCluster.Namespace, ctx.ICSCluster.Name)
	}
	// Define a variable to assign the API endpoints of control plane
	// machines as they are discovered.
	var apiEndpointList []clusterv1.APIEndpoint //nolint:prealloc

	// Iterate over the cluster's control plane CAPI machines.
	for _, icsMachine := range icsMachines {
		// Get the ICSMachine for the CAPI Machine resource.
		machine, err := infrautilv1.GetOwnerMachine(ctx, ctx.Client, icsMachine.ObjectMeta)
		if err != nil {
			if infrautilv1.IsNotFoundError(err) {
				continue
			}
			return false, errors.Wrapf(err,
				"failed to get Machine for icsMachine %s/%s/%s",
				ctx.ICSCluster.Namespace, ctx.ICSCluster.Name, icsMachine.Name)
		}

		// Only machines with bootstrap data will have an IP address.
		if machine == nil || machine.Spec.Bootstrap.DataSecretName == nil {
			if machine != nil {
				ctx.Logger.V(5).Info(
					"skipping machine while looking for IP address",
					"reason", "bootstrap.DataSecretName is nil",
					"machine-name", machine.Name)
			}
			continue
		}
		// Get the ICSMachine's preferred IP address.
		ipAddr, err := infrautilv1.GetMachinePreferredIPAddress(icsMachine)
		if err != nil {
			if err == infrautilv1.ErrNoMachineIPAddr {
				continue
			}
			return false, errors.Wrapf(err,
				"failed to get preferred IP address for ICSMachine %s %s/%s",
				icsMachine.GroupVersionKind(),
				icsMachine.Namespace,
				icsMachine.Name)
		}
		// Append the control plane machine's IP address to the list of API
		// endpoints for this cluster so that they can be read into the
		// analogous CAPI cluster via an unstructured reader.
		apiEndpoint := clusterv1.APIEndpoint{
			Host: ipAddr,
			Port: defaultAPIEndpointPort,
		}
		apiEndpointList = append(apiEndpointList, apiEndpoint)
		ctx.Logger.V(3).Info(
			"found API endpoint via control plane machine",
			"host", apiEndpoint.Host,
			"port", apiEndpoint.Port)
	}

	// The reconciliation is only successful if some API endpoints were
	// discovered. Otherwise return an error so the cluster is requeued
	// for reconciliation.
	if len(apiEndpointList) == 0 {
		return false, errors.Wrapf(err,
			"failed to reconcile API endpoints for %s/%s",
			ctx.ICSCluster.Namespace, ctx.ICSCluster.Name)
	}

	// Update the icsCluster's list of APIEndpoints.
	ctx.ICSCluster.Spec.ControlPlaneEndpoint = apiEndpointList[0]

	cluster := &clusterv1.Cluster{}
	clusterKey := client.ObjectKey{Namespace: ctx.Cluster.Namespace, Name: ctx.Cluster.Name}
	if err := ctx.Client.Get(ctx, clusterKey, cluster); err != nil {
		ctx.Logger.Error(err, "failed to get cluster object while reconciling api endpoint")
		return false, nil
	}
	if ctx.ICSCluster.Spec.ControlPlaneEndpoint.Host != cluster.Spec.ControlPlaneEndpoint.Host ||
		ctx.ICSCluster.Spec.ControlPlaneEndpoint.Port != cluster.Spec.ControlPlaneEndpoint.Port {
		// Create the patch helper.
		patchHelper, err := patch.NewHelper(cluster, r.Client)
		if err != nil {
			return false, errors.Wrapf(err,"failed to init patch helper for the cluster object")
		}
		cluster.Spec.ControlPlaneEndpoint = *ctx.ICSCluster.Spec.ControlPlaneEndpoint.DeepCopy()
		err = patchHelper.Patch(ctx, cluster)
		if err != nil {
			return false, errors.Wrapf(err,"failed to patch the cluster object")
		}
		apiServer := fmt.Sprintf("https://%s:%d", ctx.ICSCluster.Spec.ControlPlaneEndpoint.Host,
			ctx.ICSCluster.Spec.ControlPlaneEndpoint.Port)
		secret := &corev1.Secret{}
		secretKey := apitypes.NamespacedName{
			Namespace: ctx.ICSCluster.Namespace,
			Name:      ctx.ICSCluster.Name + "-kubeconfig",
		}
		if err := ctx.Client.Get(ctx, secretKey, secret); err != nil {
			return false, errors.Wrapf(err, "failed to retrieve kubeconfig secret for the cluster %s/%s",
				ctx.ICSCluster.Namespace, ctx.ICSCluster.Name)
		}
		value, ok := secret.Data["value"]
		if !ok {
			return false, errors.New("error retrieving kubeconfig data: secret value key is missing")
		}
		data := strings.Replace(string(value), "https://127.0.0.1:6443", apiServer, -1)
		secret.Data["value"] = []byte(data)

		err = r.Client.Update(ctx, secret.DeepCopy())
		if err != nil {
			return false, errors.Wrapf(err,"failed to update the secret object")
		}
	}
	ctx.Logger.Info(
		"ControlPlaneEndpoin discovered via control plane machine",
		"controlPlaneEndpoint", ctx.ICSCluster.Spec.ControlPlaneEndpoint)

	return true, nil
}

var (
	// apiServerTriggers is used to prevent multiple goroutines for a single
	// Cluster that poll to see if the target API server is online.
	apiServerTriggers   = map[apitypes.UID]struct{}{}
	apiServerTriggersMu sync.Mutex
)

func (r clusterReconciler) reconcileICSClusterWhenAPIServerIsOnline(ctx *context.ClusterContext) {
	if conditions.IsTrue(ctx.Cluster, clusterv1.ControlPlaneInitializedCondition) {
		ctx.Logger.Info("skipping reconcile when API server is online",
			"reason", "controlPlaneInitialized")
		return
	}
	apiServerTriggersMu.Lock()
	defer apiServerTriggersMu.Unlock()
	if _, ok := apiServerTriggers[ctx.Cluster.UID]; ok {
		ctx.Logger.Info("skipping reconcile when API server is online",
			"reason", "alreadyPolling")
		return
	}
	apiServerTriggers[ctx.Cluster.UID] = struct{}{}
	go func() {
		// Block until the target API server is online.
		ctx.Logger.Info("start polling API server for online check")
		wait.PollImmediateInfinite(time.Second * 1, func() (bool, error) { return r.isAPIServerOnline(ctx), nil }) // nolint:errcheck
		ctx.Logger.Info("stop polling API server for online check")
		ctx.Logger.Info("triggering GenericEvent", "reason", "api-server-online")
		eventChannel := ctx.GetGenericEventChannelFor(ctx.ICSCluster.GetObjectKind().GroupVersionKind())
		eventChannel <- event.GenericEvent{
			Object: ctx.ICSCluster,
		}

		// Once the control plane has been marked as initialized it is safe to
		// remove the key from the map that prevents multiple goroutines from
		// polling the API server to see if it is online.
		ctx.Logger.Info("start polling for control plane initialized")
		wait.PollImmediateInfinite(time.Second * 1, func() (bool, error) { return r.isControlPlaneInitialized(ctx), nil }) // nolint:errcheck
		ctx.Logger.Info("stop polling for control plane initialized")
		apiServerTriggersMu.Lock()
		delete(apiServerTriggers, ctx.Cluster.UID)
		apiServerTriggersMu.Unlock()
	}()
}

func (r clusterReconciler) isAPIServerOnline(ctx *context.ClusterContext) bool {
	if kubeClient, err := infrautilv1.NewKubeClient(ctx, ctx.Client, ctx.Cluster); err == nil {
		if _, err := kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil {
			// The target cluster is online. To make sure the correct control
			// plane endpoint information is logged, it is necessary to fetch
			// an up-to-date Cluster resource. If this fails, then set the
			// control plane endpoint information to the values from the
			// ICSCluster resource, as it must have the correct information
			// if the API server is online.
			cluster := &clusterv1.Cluster{}
			clusterKey := client.ObjectKey{Namespace: ctx.Cluster.Namespace, Name: ctx.Cluster.Name}
			if err := ctx.Client.Get(ctx, clusterKey, cluster); err != nil {
				cluster = ctx.Cluster.DeepCopy()
				cluster.Spec.ControlPlaneEndpoint.Host = ctx.ICSCluster.Spec.ControlPlaneEndpoint.Host
				cluster.Spec.ControlPlaneEndpoint.Port = ctx.ICSCluster.Spec.ControlPlaneEndpoint.Port
				ctx.Logger.Error(err, "failed to get updated cluster object while checking if API server is online")
			}
			ctx.Logger.Info(
				"API server is online",
				"controlPlaneEndpoint", cluster.Spec.ControlPlaneEndpoint.String())
			return true
		}
	}
	return false
}

func (r clusterReconciler) syncCustomKubeConfig(ctx *context.ClusterContext) {
	if kubeClient, err := infrautilv1.NewKubeClient(ctx, ctx.Client, ctx.Cluster); err == nil {
		clusterInfo, err := kubeClient.CoreV1().ConfigMaps("kube-public").Get(ctx, "cluster-info", metav1.GetOptions{})
		if err == nil {
			defaultAPIEndpoint := "127.0.0.1:6443"
			customerConfig := clusterInfo.Data["kubeconfig"]
			if !strings.Contains(customerConfig, defaultAPIEndpoint) {
				return
			}
			syncCustom := true
			apiServer := fmt.Sprintf("%s:%d", ctx.ICSCluster.Spec.ControlPlaneEndpoint.Host,
				ctx.ICSCluster.Spec.ControlPlaneEndpoint.Port)
			data := strings.Replace(customerConfig, defaultAPIEndpoint, apiServer, -1)
			klog.Infof("DavidWang# kubeconfig: %s", data)
			clusterInfo.Data["kubeconfig"] = data
			_, err = kubeClient.CoreV1().ConfigMaps("kube-public").Update(ctx, clusterInfo, metav1.UpdateOptions{})
			if err != nil {
				klog.Errorf("failed to update customer cluster info %s/%s", ctx.ICSCluster.Namespace, ctx.ICSCluster.Name)
				syncCustom = false
			} else {
				ctx.Logger.Info("Sync custom cluster kubeconfig success",
					"Namespace", ctx.ICSCluster.Namespace, "Name", ctx.ICSCluster.Name)
			}
			kubeadmConfig, err := kubeClient.CoreV1().ConfigMaps("kube-system").Get(ctx, "kubeadm-config", metav1.GetOptions{})
			if err == nil {
				customerConfig = kubeadmConfig.Data["ClusterConfiguration"]
				if strings.Contains(customerConfig, defaultAPIEndpoint) {
					data = strings.Replace(customerConfig, defaultAPIEndpoint, apiServer, -1)
					kubeadmConfig.Data["ClusterConfiguration"] = data
					_, err = kubeClient.CoreV1().ConfigMaps("kube-system").Update(ctx, kubeadmConfig, metav1.UpdateOptions{})
					if err != nil {
						klog.Errorf("failed to update customer cluster kubeadm Config %s/%s", ctx.ICSCluster.Namespace, ctx.ICSCluster.Name)
						syncCustom = false
					}
				}
			}
			kubeProxy, err := kubeClient.CoreV1().ConfigMaps("kube-system").Get(ctx, "kube-proxy", metav1.GetOptions{})
			if err == nil {
				kubeConfig := kubeProxy.Data["kubeconfig.conf"]
				if strings.Contains(kubeConfig, defaultAPIEndpoint) {
					data = strings.Replace(kubeConfig, defaultAPIEndpoint, apiServer, -1)
					kubeProxy.Data["kubeconfig.conf"] = data
					_, err = kubeClient.CoreV1().ConfigMaps("kube-system").Update(ctx, kubeProxy, metav1.UpdateOptions{})
					if err != nil {
						klog.Errorf("failed to update customer cluster kubeproxy Config %s/%s", ctx.ICSCluster.Namespace, ctx.ICSCluster.Name)
						syncCustom = false
					}
				}
			}
			if syncCustom {
				klog.Infof("Sync custom cluster %s/%s apiServer Endpoint to %s", ctx.ICSCluster.Namespace,
					ctx.ICSCluster.Name, apiServer)
			}
		} else {
			klog.Errorf("failed to get customer cluster info %s/%s cluster-info configMap", ctx.ICSCluster.Namespace, ctx.ICSCluster.Name)
		}
		return
	}
	klog.Errorf("failed to connect customer cluster %s/%s", ctx.ICSCluster.Namespace, ctx.ICSCluster.Name)
}

func (r clusterReconciler) isControlPlaneInitialized(ctx *context.ClusterContext) bool {
	cluster := &clusterv1.Cluster{}
	clusterKey := client.ObjectKey{Namespace: ctx.Cluster.Namespace, Name: ctx.Cluster.Name}
	if err := ctx.Client.Get(ctx, clusterKey, cluster); err != nil {
		if !apierrors.IsNotFound(err) {
			ctx.Logger.Error(err, "failed to get updated cluster object while checking if control plane is initialized")
			return false
		}
		ctx.Logger.Info("exiting early because cluster no longer exists")
		return true
	}
	return conditions.IsTrue(ctx.Cluster, clusterv1.ControlPlaneInitializedCondition)
}

func setOwnerRefsOnICSMachines(ctx *context.ClusterContext) error {
	icsMachines, err := infrautilv1.GetICSMachinesInCluster(ctx, ctx.Client, ctx.Cluster.Namespace, ctx.Cluster.Name)
	if err != nil {
		return errors.Wrapf(err,
			"unable to list ICSMachines part of ICSCluster %s/%s", ctx.ICSCluster.Namespace, ctx.ICSCluster.Name)
	}

	patchErrors := []error{}
	for _, icsMachine := range icsMachines {
		patchHelper, err := patch.NewHelper(icsMachine, ctx.Client)
		if err != nil {
			patchErrors = append(patchErrors, err)
			continue
		}

		icsMachine.SetOwnerReferences(clusterutilv1.EnsureOwnerRef(
			icsMachine.OwnerReferences,
			metav1.OwnerReference{
				APIVersion: ctx.ICSCluster.APIVersion,
				Kind:       ctx.ICSCluster.Kind,
				Name:       ctx.ICSCluster.Name,
				UID:        ctx.ICSCluster.UID,
			}))

		if err := patchHelper.Patch(ctx, icsMachine); err != nil {
			patchErrors = append(patchErrors, err)
		}
	}
	return kerrors.NewAggregate(patchErrors)
}

// getControlPlaneMachineToClusterReq is a handler.ToRequestsFunc to be used
// to enqueue requests for reconciliation for ICSCluster to update
// its status.apiEndpoints field.
func (r clusterReconciler) getControlPlaneMachineToClusterReq(o client.Object) []reconcile.Request {
	icsMachine, ok := o.(*infrav1.ICSMachine)
	if !ok {
		r.Logger.Error(nil, fmt.Sprintf("expected a ICSMachine but got a %T", o))
		return nil
	}
	if !infrautilv1.IsControlPlaneMachine(icsMachine) {
		return nil
	}
	if len(icsMachine.Status.Addresses) == 0 {
		return nil
	}
	// Get the ICSMachine's preferred IP address.
	if _, err := infrautilv1.GetMachinePreferredIPAddress(icsMachine); err != nil {
		if err == infrautilv1.ErrNoMachineIPAddr {
			return nil
		}
		r.Logger.Error(err, "failed to get preferred IP address for ICSMachine",
			"namespace", icsMachine.Namespace, "name", icsMachine.Name)
		return nil
	}

	// Fetch the CAPI Cluster.
	cluster, err := clusterutilv1.GetClusterFromMetadata(r, r.Client, icsMachine.ObjectMeta)
	if err != nil {
		r.Logger.Error(err, "ICSMachine is missing cluster label or cluster does not exist",
			"namespace", icsMachine.Namespace, "name", icsMachine.Name)
		return nil
	}

	if conditions.IsTrue(cluster, clusterv1.ControlPlaneInitializedCondition) {
		return nil
	}

	if !cluster.Spec.ControlPlaneEndpoint.IsZero() {
		return nil
	}

	// Fetch the ICSCluster
	icsCluster := &infrav1.ICSCluster{}
	icsClusterKey := client.ObjectKey{
		Namespace: icsMachine.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	if err := r.Client.Get(r, icsClusterKey, icsCluster); err != nil {
		r.Logger.Error(err, "failed to get ICSCluster",
			"namespace", icsClusterKey.Namespace, "name", icsClusterKey.Name)
		return nil
	}

	if !icsCluster.Spec.ControlPlaneEndpoint.IsZero() {
		return nil
	}
	requests := []reconcile.Request{}
	req := reconcile.Request{
		NamespacedName: apitypes.NamespacedName{
			Name:      icsClusterKey.Name,
			Namespace: icsClusterKey.Namespace,
		},
	}
	requests = append(requests, req)

	return requests
}
