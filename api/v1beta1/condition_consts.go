/*
24 The Kubernetes Authors.

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

import clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

// Conditions and condition Reasons for the ICSMachine and the ICSVM object.
//
// NOTE: ICSMachine wraps a ICS VM, some we are using a unique set of conditions and reasons in order
// to ensure a consistent UX; differences between the two objects will be highlighted in the comments.

const (
	// VMProvisionedCondition documents the status of the provisioning of a ICSMachine and its underlying ICSVM.
	VMProvisionedCondition clusterv1.ConditionType = "VMProvisioned"

	// WaitingForClusterInfrastructureReason (Severity=Info) documents a ICSMachine waiting for the cluster
	// infrastructure to be ready before starting the provisioning process.
	//
	// NOTE: This reason does not apply to ICSVM (this state happens before the ICSVM is actually created).
	WaitingForClusterInfrastructureReason = "WaitingForClusterInfrastructure"

	// WaitingForBootstrapDataReason (Severity=Info) documents a ICSMachine waiting for the bootstrap
	// script to be ready before starting the provisioning process.
	//
	// NOTE: This reason does not apply to ICSVM (this state happens before the ICSVM is actually created).
	WaitingForBootstrapDataReason = "WaitingForBootstrapData"

	// WaitingForStaticIPAllocationReason (Severity=Info) documents a ICSVM waiting for the allocation of
	// a static IP address.
	WaitingForStaticIPAllocationReason = "WaitingForStaticIPAllocation"

	// CloningReason documents (Severity=Info) a ICSMachine/ICSVM currently executing the clone operation.
	CloningReason = "Cloning"

	// CloningFailedReason (Severity=Warning) documents a ICSMachine/ICSVM controller detecting
	// an error while provisioning; those kind of errors are usually transient and failed provisioning
	// are automatically re-tried by the controller.
	CloningFailedReason = "CloningFailed"

	// PoweringOnReason documents (Severity=Info) a ICSMachine/ICSVM currently executing the power on sequence.
	PoweringOnReason = "PoweringOn"

	// PoweringOnFailedReason (Severity=Warning) documents a ICSMachine/ICSVM controller detecting
	// an error while powering on; those kind of errors are usually transient and failed provisioning
	// are automatically re-tried by the controller.
	PoweringOnFailedReason = "PoweringOnFailed"

	// TaskFailure (Severity=Warning) documents a ICSMachine/ICS task failure; the reconcile look will automatically
	// retry the operation, but a user intervention might be required to fix the problem.
	TaskFailure = "TaskFailure"

	// WaitingForNetworkAddressesReason (Severity=Info) documents a ICSMachine waiting for the the machine network
	// settings to be reported after machine being powered on.
	//
	// NOTE: This reason does not apply to ICSVM (this state happens after the ICSVM is in ready state).
	WaitingForNetworkAddressesReason = "WaitingForNetworkAddresses"

	// TagsAttachmentFailedReason (Severity=Error) documents a ICSMachine/ICSVM tags attachment failure.
	TagsAttachmentFailedReason = "TagsAttachmentFailed"
)

// Conditions and Reasons related to utilizing a ICSIdentity to make connections to a ICenter.
// Can currently be used by ICSCluster and ICSVM.
const (
	// ICenterAvailableCondition documents the connectivity with icenter
	// for a given resource.
	ICenterAvailableCondition clusterv1.ConditionType = "ICenterAvailable"

	// ICenterUnreachableReason (Severity=Error) documents a controller detecting
	// issues with ICenter reachability.
	ICenterUnreachableReason = "ICenterUnreachable"
)

const (
	// ClusterModulesAvailableCondition documents the availability of cluster modules for the ICSCluster object.
	ClusterModulesAvailableCondition clusterv1.ConditionType = "ClusterModulesAvailable"

	// MissingICenterVersionReason (Severity=Warning) documents a controller detecting
	//  the scenario in which the iCenter version is not set in the status of the ICSCluster object.
	MissingICenterVersionReason = "MissingICenterVersion"

	// ICenterVersionIncompatibleReason (Severity=Info) documents the case where the iCenter version of the
	// ICSCluster object does not support cluster modules.
	ICenterVersionIncompatibleReason = "ICenterVersionIncompatible"

	// ClusterModuleSetupFailedReason (Severity=Warning) documents a controller detecting
	// issues when setting up anti-affinity constraints via cluster modules for objects
	// belonging to the cluster.
	ClusterModuleSetupFailedReason = "ClusterModuleSetupFailed"
)

const (
	// CredentialsAvailableCondidtion is used by ICSClusterIdentity when a credential
	// secret is available and unused by other ICSClusterIdentities.
	CredentialsAvailableCondidtion clusterv1.ConditionType = "CredentialsAvailable"

	// SecretNotAvailableReason is used when the secret referenced by the ICSClusterIdentity cannot be found.
	SecretNotAvailableReason = "SecretNotAvailable"

	// SecretOwnerReferenceFailedReason is used for errors while updating the owner reference of the secret.
	SecretOwnerReferenceFailedReason = "SecretOwnerReferenceFailed"

	// SecretAlreadyInUseReason is used when another ICSClusterIdentity is using the secret.
	SecretAlreadyInUseReason = "SecretInUse"
)

const (
	// PlacementConstraintMetCondition documents whether the placement constraint is configured correctly or not.
	PlacementConstraintMetCondition clusterv1.ConditionType = "PlacementConstraintMet"
)
