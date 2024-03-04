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

package util

import (
	"context"

	"github.com/pkg/errors"
	apitypes "k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
)

// GetICSClusterFromICSMachine gets the infrastructure.cluster.x-k8s.io.ICSCluster resource for the given ICSMachine.
func GetICSClusterFromICSMachine(ctx context.Context, c client.Client, machine *infrav1.ICSMachine) (*infrav1.ICSCluster, error) {
	clusterName := machine.Labels[clusterv1.ClusterLabelName]
	if clusterName == "" {
		return nil, errors.Errorf("error getting ICSCluster name from ICSMachine %s/%s",
			machine.Namespace, machine.Name)
	}
	namespacedName := apitypes.NamespacedName{
		Namespace: machine.Namespace,
		Name:      clusterName,
	}
	cluster := &clusterv1.Cluster{}
	if err := c.Get(ctx, namespacedName, cluster); err != nil {
		return nil, err
	}

	icsClusterKey := apitypes.NamespacedName{
		Namespace: machine.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	icsCluster := &infrav1.ICSCluster{}
	err := c.Get(ctx, icsClusterKey, icsCluster)
	return icsCluster, err
}
