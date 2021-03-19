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

package v1alpha3

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ICSMachineTemplateSpec defines the desired state of ICSMachineTemplate
type ICSMachineTemplateSpec struct {
	Template ICSMachineTemplateResource `json:"template"`
	Ipam     IPAMSpec                   `json:"ipam,omitempty"`
}

// ICSMachineTemplateStatus defines the observed state of ICSMachineTemplate
type ICSMachineTemplateStatus struct {
	// LastUpdated identifies when this status was last observed.
	// +optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`

	// IPPoolStatus defines the observed state of IPPool.
	Pools       []IPPoolStatus `json:"pools,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=icsmachinetemplates,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion

// ICSMachineTemplate is the Schema for the icsmachinetemplates API
type ICSMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ICSMachineTemplateSpec   `json:"spec,omitempty"`
	Status ICSMachineTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ICSMachineTemplateList contains a list of ICSMachineTemplate
type ICSMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ICSMachineTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ICSMachineTemplate{}, &ICSMachineTemplateList{})
}
