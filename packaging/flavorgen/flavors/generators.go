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

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
	"github.com/inspur-ics/cluster-api-provider-ics/packaging/flavorgen/flavors/env"
	"github.com/inspur-ics/cluster-api-provider-ics/packaging/flavorgen/flavors/util"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/identity"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
)

func newICSCluster() infrav1.ICSCluster {
	isSecure := true
	return infrav1.ICSCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: infrav1.GroupVersion.String(),
			Kind:       util.TypeToKind(&infrav1.ICSCluster{}),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      env.ClusterNameVar,
			Namespace: env.NamespaceVar,
		},
		Spec: infrav1.ICSClusterSpec{
			CloudName:     env.ICSServerVar,
			IdentityRef: &infrav1.ICSIdentityReference{
				Name: fmt.Sprintf("%s-cloud-config", env.ClusterNameVar),
				Kind: infrav1.SecretKind,
			},
			Insecure:        &isSecure,
		},
	}
}

func newICSClusterWithLoadBalancer() infrav1.ICSCluster {
	isSecure := true
	return infrav1.ICSCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: infrav1.GroupVersion.String(),
			Kind:       util.TypeToKind(&infrav1.ICSCluster{}),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      env.ClusterNameVar,
			Namespace: env.NamespaceVar,
		},
		Spec: infrav1.ICSClusterSpec{
			CloudName:     env.ICSServerVar,
			IdentityRef: &infrav1.ICSIdentityReference{
				Name: fmt.Sprintf("%s-cloud-config", env.ClusterNameVar),
				Kind: infrav1.SecretKind,
			},
			Insecure:        &isSecure,
			ControlPlaneEndpoint: clusterv1.APIEndpoint{
				Host: env.ControlPlaneEndpointVar,
				Port: 6443,
			},
		},
	}
}

func newCluster(icsCluster infrav1.ICSCluster, controlPlane *controlplanev1.KubeadmControlPlane) clusterv1.Cluster {
	cluster := clusterv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       util.TypeToKind(&clusterv1.Cluster{}),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      env.ClusterNameVar,
			Namespace: env.NamespaceVar,
			Labels:    clusterLabels(),
		},
		Spec: clusterv1.ClusterSpec{
			ClusterNetwork: &clusterv1.ClusterNetwork{
				Pods: &clusterv1.NetworkRanges{
					CIDRBlocks: []string{env.DefaultClusterCIDR},
				},
			},
			InfrastructureRef: &corev1.ObjectReference{
				APIVersion: icsCluster.GroupVersionKind().GroupVersion().String(),
				Kind:       icsCluster.Kind,
				Name:       icsCluster.Name,
			},
		},
	}
	if controlPlane != nil {
		cluster.Spec.ControlPlaneRef = &corev1.ObjectReference{
			APIVersion: controlPlane.GroupVersionKind().GroupVersion().String(),
			Kind:       controlPlane.Kind,
			Name:       controlPlane.Name,
		}
	}
	return cluster
}

func clusterLabels() map[string]string {
	return map[string]string{"cluster.x-k8s.io/cluster-name": env.ClusterNameVar}
}

func newICSMachineTemplate(templateName string) infrav1.ICSMachineTemplate {
	return infrav1.ICSMachineTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      templateName,
			Namespace: env.NamespaceVar,
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: infrav1.GroupVersion.String(),
			Kind:       util.TypeToKind(&infrav1.ICSMachineTemplate{}),
		},
		Spec: infrav1.ICSMachineTemplateSpec{
			Template: infrav1.ICSMachineTemplateResource{
				Spec: defaultVirtualMachineSpec(),
			},
		},
	}
}

func defaultVirtualMachineSpec() infrav1.ICSMachineSpec {
	return infrav1.ICSMachineSpec{
		VirtualMachineCloneSpec: defaultVirtualMachineCloneSpec(),
	}
}

func defaultVirtualMachineCloneSpec() infrav1.VirtualMachineCloneSpec {
	return infrav1.VirtualMachineCloneSpec{
		CloudName:         env.ICSServerVar,
		IdentityRef: &infrav1.ICSIdentityReference{
			Name: fmt.Sprintf("%s-cloud-config", env.ClusterNameVar),
			Kind: infrav1.SecretKind,
		},
		Datacenter:        env.ICSDataCenterVar,
		Network: infrav1.NetworkSpec{
			Devices: []infrav1.NetworkDeviceSpec{
				{
					NetworkName: env.ICSNetworkVar,
					DHCP4:       true,
					DHCP6:       false,
				},
			},
		},
		CloneMode:         infrav1.LinkedClone,
		NumCPUs:           2,
		DiskGiB:           20,
		MemoryMiB:         8192,
		Template:          env.ICSTemplateVar,
		Datastore:         env.ICSDatastoreVar,
	}
}

func defaultKubeadmInitSpec(files []bootstrapv1.File) bootstrapv1.KubeadmConfigSpec {
	return bootstrapv1.KubeadmConfigSpec{
		InitConfiguration: &bootstrapv1.InitConfiguration{
			NodeRegistration: defaultNodeRegistrationOptions(),
		},
		JoinConfiguration: &bootstrapv1.JoinConfiguration{
			NodeRegistration: defaultNodeRegistrationOptions(),
		},
		ClusterConfiguration: &bootstrapv1.ClusterConfiguration{
			APIServer: bootstrapv1.APIServer{
				ControlPlaneComponent: defaultControlPlaneComponent(),
			},
			ControllerManager: defaultControlPlaneComponent(),
		},
		Users:                    defaultUsers(),
		PreKubeadmCommands:       defaultPreKubeadmCommands(),
		UseExperimentalRetryJoin: true,
		Files:                    files,
	}
}

func newKubeadmWorkConfigTemplate(templateName string, addUsers bool) bootstrapv1.KubeadmConfigTemplate {
	template := bootstrapv1.KubeadmConfigTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      templateName,
			Namespace: env.NamespaceVar,
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: bootstrapv1.GroupVersion.String(),
			Kind:       util.TypeToKind(&bootstrapv1.KubeadmConfigTemplate{}),
		},
		Spec: bootstrapv1.KubeadmConfigTemplateSpec{
			Template: bootstrapv1.KubeadmConfigTemplateResource{
				Spec: bootstrapv1.KubeadmConfigSpec{
					JoinConfiguration: &bootstrapv1.JoinConfiguration{
						NodeRegistration: defaultNodeRegistrationOptions(),
					},
					PreKubeadmCommands: defaultPreKubeadmCommands(),
				},
			},
		},
	}
	if addUsers {
		template.Spec.Template.Spec.Users = defaultUsers()
	}
	return template
}

func defaultNodeRegistrationOptions() bootstrapv1.NodeRegistrationOptions {
	return bootstrapv1.NodeRegistrationOptions{
		Name:             "{{ ds.meta_data.hostname }}",
		CRISocket:        "/var/run/containerd/containerd.sock",
		KubeletExtraArgs: defaultExtraArgs(),
	}
}

func defaultUsers() []bootstrapv1.User {
	return []bootstrapv1.User{
		{
			Name: "capics",
			Sudo: pointer.StringPtr("ALL=(ALL) NOPASSWD:ALL"),
			SSHAuthorizedKeys: []string{
				env.ICSSSHAuthorizedKeysVar,
			},
		},
	}
}

func defaultControlPlaneComponent() bootstrapv1.ControlPlaneComponent {
	return bootstrapv1.ControlPlaneComponent{
		ExtraArgs: defaultExtraArgs(),
	}
}

func defaultExtraArgs() map[string]string {
	return map[string]string{
		"cloud-provider": "external",
	}
}

func defaultPreKubeadmCommands() []string {
	return []string{
		"hostname \"{{ ds.meta_data.hostname }}\"",
		"echo \"::1         ipv6-localhost ipv6-loopback\" >/etc/hosts",
		"echo \"127.0.0.1   localhost\" >>/etc/hosts",
		"echo \"127.0.0.1   {{ ds.meta_data.hostname }}\" >>/etc/hosts",
		"echo \"{{ ds.meta_data.hostname }}\" >/etc/hostname",
	}
}

func newIdentitySecret() corev1.Secret {
	return corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       util.TypeToKind(&corev1.Secret{}),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: env.NamespaceVar,
			Name:      fmt.Sprintf("%s-cloud-config", env.ClusterNameVar),
		},
		StringData: map[string]string{
			identity.CloudsSecretKey: env.ICSServerConfig,
			identity.CaSecretKey: env.ICSServerCa,
		},
	}
}

func newMachineDeployment(cluster clusterv1.Cluster, machineTemplate infrav1.ICSMachineTemplate, bootstrapTemplate bootstrapv1.KubeadmConfigTemplate) clusterv1.MachineDeployment {
	return clusterv1.MachineDeployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       util.TypeToKind(&clusterv1.MachineDeployment{}),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      env.ClusterNameVar + env.MachineDeploymentNameSuffix,
			Labels:    clusterLabels(),
			Namespace: env.NamespaceVar,
		},
		Spec: clusterv1.MachineDeploymentSpec{
			ClusterName: env.ClusterNameVar,
			Replicas:    pointer.Int32Ptr(int32(555)),
			Template: clusterv1.MachineTemplateSpec{
				ObjectMeta: clusterv1.ObjectMeta{
					Labels: clusterLabels(),
				},
				Spec: clusterv1.MachineSpec{
					Version:     pointer.StringPtr(env.KubernetesVersionVar),
					ClusterName: cluster.Name,
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: bootstrapTemplate.GroupVersionKind().GroupVersion().String(),
							Kind:       bootstrapTemplate.Kind,
							Name:       bootstrapTemplate.Name,
						},
					},
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: machineTemplate.GroupVersionKind().GroupVersion().String(),
						Kind:       machineTemplate.Kind,
						Name:       machineTemplate.Name,
					},
				},
			},
		},
	}
}

func newKubeadmControlplane(replicas int, infraTemplate infrav1.ICSMachineTemplate, files []bootstrapv1.File) controlplanev1.KubeadmControlPlane {
	return controlplanev1.KubeadmControlPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: controlplanev1.GroupVersion.String(),
			Kind:       util.TypeToKind(&controlplanev1.KubeadmControlPlane{}),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      env.ClusterNameVar,
			Namespace: env.NamespaceVar,
		},
		Spec: controlplanev1.KubeadmControlPlaneSpec{
			Replicas: pointer.Int32Ptr(int32(replicas)),
			Version:  env.KubernetesVersionVar,
			MachineTemplate: controlplanev1.KubeadmControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{
					APIVersion: infraTemplate.GroupVersionKind().GroupVersion().String(),
					Kind:       infraTemplate.Kind,
					Name:       infraTemplate.Name,
				},
			},
			KubeadmConfigSpec: defaultKubeadmInitSpec(files),
		},
	}
}
