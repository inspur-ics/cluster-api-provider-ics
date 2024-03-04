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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

const (
	// ClusterFinalizer allows ReconcileICSCluster to clean up ics
	// resources associated with ICSCluster before removing it from the
	// API server.
	ClusterFinalizer = "icscluster.infrastructure.cluster.x-k8s.io"
)

// ICenterVersion conveys the API version of the iCenter instance.
type ICenterVersion string

func NewICenterVersion(version string) ICenterVersion {
	return ICenterVersion(version)
}

// ICSClusterSpec defines the desired state of ICSCluster
type ICSClusterSpec struct {
	// The name of the cloud to use from the clouds secret
	// +optional
	CloudName string `json:"cloudName"`

	// IdentityRef is a reference to either a Secret that contains
	// the identity to use when reconciling the cluster.
	// +optional
	IdentityRef *ICSIdentityReference `json:"identityRef,omitempty"`

	// Insecure is a flag that controls whether or not to validate the
	// ics server's certificate.
	// +optional
	Insecure *bool `json:"insecure,omitempty"`

	// ControlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	ControlPlaneEndpoint clusterv1.APIEndpoint `json:"controlPlaneEndpoint"`

	// ClusterModules hosts information regarding the anti-affinity ICS constructs
	// for each of the objects responsible for creation of VM objects belonging to the cluster.
	// +optional
	ClusterModules []ClusterModule `json:"clusterModules,omitempty"`
}

// ClusterModule holds the anti affinity construct `ClusterModule` identifier
// in use by the VMs owned by the object referred by the TargetObjectName field.
type ClusterModule struct {
	// ControlPlane indicates whether the referred object is responsible for control plane nodes.
	// Currently, only the KubeadmControlPlane objects have this flag set to true.
	// Only a single object in the slice can have this value set to true.
	ControlPlane bool `json:"controlPlane"`

	// TargetObjectName points to the object that uses the Cluster Module information to enforce
	// anti-affinity amongst its descendant VM objects.
	TargetObjectName string `json:"targetObjectName"`

	// ModuleUUID is the unique identifier of the `ClusterModule` used by the object.
	ModuleUUID string `json:"moduleUUID"`
}

// ICSClusterStatus defines the observed state of ICSCluster
type ICSClusterStatus struct {
	// +optional
	Ready bool `json:"ready,omitempty"`

	// Conditions defines current service state of the ICSCluster.
	// +optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`

	// ICenterVersion defines the version of the iCenter server defined in the spec.
	ICenterVersion ICenterVersion `json:"iCenterVersion,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=icsclusters,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.ready",description="Cluster infrastructure is ready for ICSMachine"
// +kubebuilder:printcolumn:name="CloudName",type="string",JSONPath=".spec.cloudName",description="Server is the address of the iCenter endpoint."
// +kubebuilder:printcolumn:name="ControlPlaneEndpoint",type="string",JSONPath=".spec.controlPlaneEndpoint[0]",description="API Endpoint",priority=1

// ICSCluster is the Schema for the icsclusters API
type ICSCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ICSClusterSpec   `json:"spec,omitempty"`
	Status ICSClusterStatus `json:"status,omitempty"`
}

func (m *ICSCluster) GetConditions() clusterv1.Conditions {
	return m.Status.Conditions
}

func (m *ICSCluster) SetConditions(conditions clusterv1.Conditions) {
	m.Status.Conditions = conditions
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
