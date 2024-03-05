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

package env

const (
	ClusterNameVar               = "${CLUSTER_NAME}"
	ClusterClassNameVar          = "${CLUSTER_CLASS_NAME}"
	ControlPlaneMachineCountVar  = "${CONTROL_PLANE_MACHINE_COUNT}"
	ControlPlaneEndpointVar      = "${CONTROL_PLANE_ENDPOINT_IP}"
	DefaultClusterCIDR           = "192.168.0.0/16"
	DefaultDiskGiB               = "${MACHINE_DISK_SIZE}"
	DefaultMemoryMiB             = "${MACHINE_MEMORY_SIZE}"
	DefaultNumCPUs               = "${MACHINE_CPU_COUNT}"
	KubernetesVersionVar         = "${KUBERNETES_VERSION}"
	MachineDeploymentNameSuffix  = "-md-0"
	NamespaceVar                 = "${NAMESPACE}"
	ICSDataCenterVar             = "${ICS_DATACENTER}"
	ICSDatastoreVar              = "${ICS_DATASTORE}"
	ICSNetworkVar                = "${ICS_NETWORK}"
	ICSServerVar                 = "${ICS_SERVER}"
	ICSSSHAuthorizedKeysVar      = "${ICS_SSH_AUTHORIZED_KEY}"
	ICSStoragePolicyVar          = "${ICS_STORAGE_POLICY}"
	ICSTemplateVar               = "${ICS_TEMPLATE}"
	WorkerMachineCountVar        = "${WORKER_MACHINE_COUNT}"
	ICSServerConfig              = "${ICS_SERVER_YAML_B64}"
	ICSServerCa                  = "${ICS_SERVER_CACERT_B64}"
)
