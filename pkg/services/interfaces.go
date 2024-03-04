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

package services

import (
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
)

// ICSMachineService is used for ICS VM lifecycle and syncing with ICSMachine types.
type ICSMachineService interface {
	FetchICSMachine(client client.Client, name types.NamespacedName) (context.MachineContext, error)
	FetchICSCluster(client client.Client, cluster *clusterv1.Cluster, machineContext context.MachineContext) (context.MachineContext, error)
	ReconcileDelete(ctx context.MachineContext) error
	SyncFailureReason(ctx context.MachineContext) (bool, error)
	ReconcileNormal(ctx context.MachineContext) (bool, error)
	GetHostInfo(ctx context.MachineContext) (string, error)
}

// VirtualMachineService is a service for creating/updating/deleting virtual
// machines on ICS.
type VirtualMachineService interface {
	// ReconcileVM reconciles a VM with the intended state.
	ReconcileVM(ctx *context.VMContext) (infrav1.VirtualMachine, error)

	// DestroyVM powers off and removes a VM from the inventory.
	DestroyVM(ctx *context.VMContext) (infrav1.VirtualMachine, error)
}
