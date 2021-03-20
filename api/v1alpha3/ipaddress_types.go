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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DataFinalizer allows IPAddressReconciler to clean up resources
	// associated with IPAddress before removing it from the apiserver.
	IPAddressFinalizer = "ipaddress.infrastructure.cluster.x-k8s.io"
)

// IPAddressSpec defines the desired state of IPAddress.
type IPAddressSpec struct {

	// VM points to the object the ICSVM was created for.
	VMRef corev1.ObjectReference `json:"vmRef"`

	// Template is the ICSMachineTemplate this was generated from.
	TemplateRef corev1.ObjectReference `json:"templateRef"`

	// +kubebuilder:validation:Maximum=128
	// Prefix is the mask of the network as integer (max 128)
	Prefix int `json:"prefix,omitempty"`

	// Gateway is the gateway ip address
	Gateway *IPAddressStr `json:"gateway,omitempty"`

	// Address contains the IP address
	Address IPAddressStr `json:"address"`

	// MACAddr is the MAC address used by this device.
	// It is generally a good idea to omit this field and allow a MAC address
	// to be generated.
	// Please note that this value must use the InCloud Sphere OUI to work with the
	// in-tree ics cloud provider.
	// +optional
	MACAddr string `json:"macAddr,omitempty"`

	// DNSServers is the list of dns servers
	DNSServers []IPAddressStr `json:"dnsServers,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=ipaddresses,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion

// IPAddress is the Schema for the ipaddresses API
type IPAddress struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec IPAddressSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// IPAddressList contains a list of IPAddress
type IPAddressList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPAddress `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IPAddress{}, &IPAddressList{})
}
