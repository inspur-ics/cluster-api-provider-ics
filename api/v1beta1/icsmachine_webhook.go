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
	"net"
	"strings"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var _ = logf.Log.WithName("icsmachine-resource")

func (r *ICSMachine) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-icsmachine,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=icsmachines,versions=v1beta1,name=validation.icsmachine.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

var (
	_ webhook.Defaulter = &ICSMachine{}
	_ webhook.Validator = &ICSMachine{}
)

// Default satisfies the defaulting webhook interface.
func (r *ICSMachine) Default() {
	if r.Spec.IdentityRef != nil && r.Spec.IdentityRef.Kind == "" {
		r.Spec.IdentityRef.Kind = defaultIdentityRefKind
	}
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (r *ICSMachine) ValidateCreate() error {
	var allErrs field.ErrorList

	if r.Spec.IdentityRef != nil && r.Spec.IdentityRef.Kind != defaultIdentityRefKind {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "identityRef", "kind"), "must be a Secret"))
	}

	return aggregateObjErrors(r.GroupVersionKind().GroupKind(), r.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (r *ICSMachine) ValidateUpdate(old runtime.Object) error {
	newICSMachine, err := runtime.DefaultUnstructuredConverter.ToUnstructured(r)
	if err != nil {
		return apierrors.NewInvalid(GroupVersion.WithKind("ICSMachine").GroupKind(), r.Name, field.ErrorList{
			field.InternalError(nil, errors.Wrap(err, "failed to convert new ICSMachine to unstructured object")),
		})
	}
	oldICSMachine, err := runtime.DefaultUnstructuredConverter.ToUnstructured(old)
	if err != nil {
		return apierrors.NewInvalid(GroupVersion.WithKind("ICSMachine").GroupKind(), r.Name, field.ErrorList{
			field.InternalError(nil, errors.Wrap(err, "failed to convert old ICSMachine to unstructured object")),
		})
	}

	var allErrs field.ErrorList

	if r.Spec.IdentityRef != nil && r.Spec.IdentityRef.Kind != defaultIdentityRefKind {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "identityRef", "kind"), "must be a Secret"))
	}

	newICSMachineSpec := newICSMachine["spec"].(map[string]interface{})
	oldICSMachineSpec := oldICSMachine["spec"].(map[string]interface{})

	// allow changes to providerID once
	if oldICSMachineSpec["providerID"] == nil {
		delete(oldICSMachineSpec, "providerID")
		delete(newICSMachineSpec, "providerID")
	}

	newICSMachineNetwork := newICSMachineSpec["network"].(map[string]interface{})
	oldICSMachineNetwork := oldICSMachineSpec["network"].(map[string]interface{})

	// allow changes to the devices..
	delete(oldICSMachineNetwork, "devices")
	delete(newICSMachineNetwork, "devices")

	// validate that IPAddrs in updaterequest are valid.
	spec := r.Spec
	for i, device := range spec.Network.Devices {
		for j, ip := range device.IPAddrs {
			if !strings.Contains(ip, "/") {
				continue
			}
			if _, _, err := net.ParseCIDR(ip); err != nil {
				allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "network", fmt.Sprintf("devices[%d]", i), fmt.Sprintf("ipAddrs[%d]", j)), ip, "ip addresses should be in the CIDR format"))
			}
		}
	}

	return aggregateObjErrors(r.GroupVersionKind().GroupKind(), r.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (r *ICSMachine) ValidateDelete() error {
	return nil
}