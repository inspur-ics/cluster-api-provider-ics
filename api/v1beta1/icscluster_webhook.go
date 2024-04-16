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

package v1beta1

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var _ = logf.Log.WithName("icscluster-resource")

func (r *ICSCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-icscluster,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=icsclusters,versions=v1beta1,name=validation.icscluster.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

var (
	_ webhook.Defaulter = &ICSCluster{}
	_ webhook.Validator = &ICSCluster{}
)

// Default satisfies the defaulting webhook interface.
func (r *ICSCluster) Default() {
	if r.Spec.IdentityRef != nil && r.Spec.IdentityRef.Kind == "" {
		r.Spec.IdentityRef.Kind = defaultIdentityRefKind
	}
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (r *ICSCluster) ValidateCreate() error {
	var allErrs field.ErrorList

	if r.Spec.IdentityRef != nil && r.Spec.IdentityRef.Kind != defaultIdentityRefKind {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "identityRef", "kind"), "must be a Secret"))
	}

	return aggregateObjErrors(r.GroupVersionKind().GroupKind(), r.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (r *ICSCluster) ValidateUpdate(oldRaw runtime.Object) error {
	var allErrs field.ErrorList
	old, ok := oldRaw.(*ICSCluster)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected an ICSCluster but got a %T", oldRaw))
	}

	if r.Spec.IdentityRef != nil && r.Spec.IdentityRef.Kind != defaultIdentityRefKind {
		allErrs = append(allErrs,
			field.Invalid(field.NewPath("spec", "identityRef", "kind"),
				r.Spec.IdentityRef, "must be a Secret"),
		)
	}

	// Allow changes to Spec.IdentityRef.Name.
	if old.Spec.IdentityRef != nil && r.Spec.IdentityRef != nil {
		old.Spec.IdentityRef.Name = ""
		r.Spec.IdentityRef.Name = ""
	}

	// Allow changes to Spec.IdentityRef if it was unset.
	if old.Spec.IdentityRef == nil && r.Spec.IdentityRef != nil {
		old.Spec.IdentityRef = &ICSIdentityReference{}
		r.Spec.IdentityRef = &ICSIdentityReference{}
	}

	if old.Spec.IdentityRef != nil && r.Spec.IdentityRef == nil {
		allErrs = append(allErrs,
			field.Invalid(field.NewPath("spec", "identityRef"),
				r.Spec.IdentityRef, "field cannot be set to nil"),
		)
	}
	return aggregateObjErrors(r.GroupVersionKind().GroupKind(), r.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (r *ICSCluster) ValidateDelete() error {
	return nil
}