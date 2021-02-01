/*
Copyright 2021 The Kubernetes Authors.

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

package v1alpha4

import (
	"reflect"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func (r *ICSMachineTemplate) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-infrastructure-cluster-x-k8s-io-v1alpha4-icsmachinetemplate,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=icsmachinetemplates,versions=v1alpha4,name=validation.icsmachinetemplate.infrastructure.x-k8s.io

var _ webhook.Validator = &ICSMachineTemplate{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *ICSMachineTemplate) ValidateCreate() error {
	var allErrs field.ErrorList
	spec := r.Spec.Template.Spec

	if spec.Network.PreferredAPIServerCIDR != "" {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "PreferredAPIServerCIDR"), spec.Network.PreferredAPIServerCIDR, "cannot be set, as it will be removed and is no longer used"))
	}

	if spec.ProviderID != nil {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "template", "spec", "providerID"), "cannot be set in templates"))
	}

	for _, device := range spec.Network.Devices {
		if len(device.IPAddrs) != 0 {
			allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "template", "spec", "network", "devices", "ipAddrs"), "cannot be set in templates"))
		}
	}

	return aggregateObjErrors(r.GroupVersionKind().GroupKind(), r.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *ICSMachineTemplate) ValidateUpdate(old runtime.Object) error {
	oldICSMachineTemplate := old.(*ICSMachineTemplate)
	if !reflect.DeepEqual(r.Spec, oldICSMachineTemplate.Spec) {
		return field.Forbidden(field.NewPath("spec"), "ICSMachineTemplateSpec is immutable")
	}

	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *ICSMachineTemplate) ValidateDelete() error {
	return nil
}
