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

package flavors

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/inspur-ics/cluster-api-provider-ics/packaging/flavorgen/flavors/env"
)

func MultiNodeTemplateWithOutLoadBalancer() []runtime.Object {
	icsCluster := newICSCluster()
	controlPlaneMachineTemplate := newICSMachineTemplate(fmt.Sprintf("%s-control-plane", env.ClusterNameVar))
	workerMachineTemplate := newICSMachineTemplate(fmt.Sprintf("%s%s", env.ClusterNameVar, env.MachineDeploymentNameSuffix))
	kubeadmControlPlane := newKubeadmControlplane(444, controlPlaneMachineTemplate, nil)
	kubeadmWorkConfigTemplate := newKubeadmWorkConfigTemplate(fmt.Sprintf("%s%s", env.ClusterNameVar, env.MachineDeploymentNameSuffix), true)
	cluster := newCluster(icsCluster, &kubeadmControlPlane)
	machineDeployment := newMachineDeployment(cluster, workerMachineTemplate, kubeadmWorkConfigTemplate)
	identitySecret := newIdentitySecret()

	MultiNodeTemplate := []runtime.Object{
		&cluster,
		&icsCluster,
		&kubeadmControlPlane,
		&controlPlaneMachineTemplate,
		&machineDeployment,
		&workerMachineTemplate,
		&kubeadmWorkConfigTemplate,
		&identitySecret,
	}

	return MultiNodeTemplate
}

func MultiNodeTemplateWithLoadBalancer() []runtime.Object {
	icsCluster := newICSClusterWithLoadBalancer()
	controlPlaneMachineTemplate := newICSMachineTemplate(fmt.Sprintf("%s-control-plane", env.ClusterNameVar))
	workerMachineTemplate := newICSMachineTemplate(fmt.Sprintf("%s%s", env.ClusterNameVar, env.MachineDeploymentNameSuffix))
	kubeadmControlPlane := newKubeadmControlplane(444, controlPlaneMachineTemplate, nil)
	kubeadmWorkConfigTemplate := newKubeadmWorkConfigTemplate(fmt.Sprintf("%s%s", env.ClusterNameVar, env.MachineDeploymentNameSuffix), true)
	cluster := newCluster(icsCluster, &kubeadmControlPlane)
	machineDeployment := newMachineDeployment(cluster, workerMachineTemplate, kubeadmWorkConfigTemplate)
	identitySecret := newIdentitySecret()

	MultiNodeTemplate := []runtime.Object{
		&cluster,
		&icsCluster,
		&kubeadmControlPlane,
		&controlPlaneMachineTemplate,
		&machineDeployment,
		&workerMachineTemplate,
		&kubeadmWorkConfigTemplate,
		&identitySecret,
	}

	return MultiNodeTemplate
}
