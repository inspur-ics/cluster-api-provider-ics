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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ClusterFinalizer allows ReconcileICSCluster to clean up ics
	// resources associated with ICSCluster before removing it from the
	// API server.
	ClusterFinalizer = "icscluster.infrastructure.cluster.x-k8s.io"
)

// ICSClusterSpec defines the desired state of ICSCluster
type ICSClusterSpec struct {
	// Server is the address of the ics endpoint.
	Server string `json:"server,omitempty"`

	// Insecure is a flag that controls whether or not to validate the
	// ics server's certificate.
	// DEPRECATED: will be removed in v1alpha4
	// +optional
	Insecure *bool `json:"insecure,omitempty"`

	// Thumbprint is the colon-separated SHA-1 checksum of the given iCenter server's host certificate
	// When provided, Insecure should not be set to true
	// +optional
	Thumbprint string `json:"thumbprint,omitempty"`

	// CloudProviderConfiguration holds the cluster-wide configuration for the
	// DEPRECATED: will be removed in v1alpha4
	// ics cloud provider.
	CloudProviderConfiguration CPIConfig `json:"cloudProviderConfiguration,omitempty"`

	// ControlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	ControlPlaneEndpoint APIEndpoint `json:"controlPlaneEndpoint"`

	// LoadBalancerRef may be used to enable a control plane load balancer
	// for this cluster.
	// When a LoadBalancerRef is provided, the ICSCluster.Status.Ready field
	// will not be true until the referenced resource is Status.Ready and has a
	// non-empty Status.Address value.
	// DEPRECATED: will be removed in v1alpha4
	// +optional
	LoadBalancerRef *corev1.ObjectReference `json:"loadBalancerRef,omitempty"`
}

// ICSClusterStatus defines the observed state of ICSClusterSpec
type ICSClusterStatus struct {
	// +optional
	Ready bool `json:"ready,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=icsclusters,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion
// +kubebuilder:subresource:status

// ICSCluster is the Schema for the icsclusters API
type ICSCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ICSClusterSpec   `json:"spec,omitempty"`
	Status ICSClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ICSClusterList contains a list of ICSCluster
type ICSClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ICSCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ICSCluster{}, &ICSClusterList{})
}
