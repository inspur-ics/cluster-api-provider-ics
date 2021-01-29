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

// Conditions and condition Reasons for the ICSCluster object.

const (
	// LoadBalancerProvisioningReason (Severity=Info) documents a ICSCluster provisioning a load balancer.
	LoadBalancerProvisioningReason = "LoadBalancerProvisioning"

	// LoadBalancerProvisioningReason (Severity=Warning) documents a ICSCluster controller detecting
	// while provisioning the load balancer; those kind of errors are usually transient and failed provisioning
	// are automatically re-tried by the controller.
	LoadBalancerProvisioningFailedReason = "LoadBalancerProvisioningFailed"

	// CCMProvisioningFailedReason (Severity=Warning) documents a ICSCluster controller detecting
	// while installing the cloud controller manager addon; those kind of errors are usually transient
	// the operation is automatically re-tried by the controller.
	CCMProvisioningFailedReason = "CCMProvisioningFailed"

	// CSIProvisioningFailedReason (Severity=Warning) documents a ICSCluster controller detecting
	// while installing the container storage interface  addon; those kind of errors are usually transient
	// the operation is automatically re-tried by the controller.
	CSIProvisioningFailedReason = "CSIProvisioningFailed"

	// ICenterUnreachableReason (Severity=Error) documents a ICSCluster controller detecting
	// issues with ICenter reachability;
	ICenterUnreachableReason = "ICenterUnreachable"
)

// Conditions and condition Reasons for the ICSMachine and the ICSVM object.
//
// NOTE: ICSMachine wraps a VMSphereVM, some we are using a unique set of conditions and reasons in order
// to ensure a consistent UX; differences between the two objects will be highlighted in the comments.

const (
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
)
