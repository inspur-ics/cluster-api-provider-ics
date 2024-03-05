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
	"reflect"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// log is for logging in this package.
var icsVMLog = logf.Log.WithName("icsvm-resource")

func (r *ICSVM) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-icsvm,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=icsvms,versions=v1beta1,name=validation.icsvm.infrastructure.x-k8s.io,sideEffects=None,admissionReviewVersions=v1beta1

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (r *ICSVM) ValidateCreate() error {
	var allErrs field.ErrorList
	spec := r.Spec

	if spec.Network.PreferredAPIServerCIDR != "" {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "PreferredAPIServerCIDR"), spec.Network.PreferredAPIServerCIDR, "cannot be set, as it will be removed and is no longer used"))
	}

	for i, device := range spec.Network.Devices {
		for j, ip := range device.IPAddrs {
			if _, _, err := net.ParseCIDR(ip); err != nil {
				allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "network", fmt.Sprintf("devices[%d]", i), fmt.Sprintf("ipAddrs[%d]", j)), ip, "ip addresses should be in the CIDR format"))
			}
		}
	}

	return aggregateObjErrors(r.GroupVersionKind().GroupKind(), r.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
//nolint:forcetypeassert
func (r *ICSVM) ValidateUpdate(old runtime.Object) error {
	newICSVM, err := runtime.DefaultUnstructuredConverter.ToUnstructured(r)
	if err != nil {
		return apierrors.NewInternalError(errors.Wrap(err, "failed to convert new ICSVM to unstructured object"))
	}
	oldICSVM, err := runtime.DefaultUnstructuredConverter.ToUnstructured(old)
	if err != nil {
		return apierrors.NewInternalError(errors.Wrap(err, "failed to convert old ICSVM to unstructured object"))
	}

	var allErrs field.ErrorList

	newICSVMSpec := newICSVM["spec"].(map[string]interface{})
	oldICSVMSpec := oldICSVM["spec"].(map[string]interface{})

	// allow changes to biosUUID
	delete(oldICSVMSpec, "biosUUID")
	delete(newICSVMSpec, "biosUUID")

	// allow changes to bootstrapRef
	delete(oldICSVMSpec, "bootstrapRef")
	delete(newICSVMSpec, "bootstrapRef")

	newICSVMNetwork := newICSVMSpec["network"].(map[string]interface{})
	oldICSVMNetwork := oldICSVMSpec["network"].(map[string]interface{})

	// allow changes to the network devices
	delete(oldICSVMNetwork, "devices")
	delete(newICSVMNetwork, "devices")

	if !reflect.DeepEqual(oldICSVMSpec, newICSVMSpec) {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec"), "cannot be modified"))
	}

	return aggregateObjErrors(r.GroupVersionKind().GroupKind(), r.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (r *ICSVM) ValidateDelete() error {
	return nil
}