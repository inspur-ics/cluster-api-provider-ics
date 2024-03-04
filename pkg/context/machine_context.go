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

package context

import (
	"fmt"

	"github.com/go-logr/logr"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/patch"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
)

type BaseMachineContext struct {
	*ControllerContext
	Logger      logr.Logger
	Cluster     *clusterv1.Cluster
	Machine     *clusterv1.Machine
	PatchHelper *patch.Helper
}

func (c *BaseMachineContext) GetCluster() *clusterv1.Cluster {
	return c.Cluster
}

func (c *BaseMachineContext) GetMachine() *clusterv1.Machine {
	return c.Machine
}

// GetLogger returns this context's logger.
func (c *BaseMachineContext) GetLogger() logr.Logger {
	return c.Logger
}

// VIMMachineContext is a Go context used with a ICSMachine.
type VIMMachineContext struct {
	*BaseMachineContext
	ICSCluster *infrav1.ICSCluster
	ICSMachine *infrav1.ICSMachine
}

// String returns ICSMachineGroupVersionKind ICSMachineNamespace/ICSMachineName.
func (c *VIMMachineContext) String() string {
	return fmt.Sprintf("%s %s/%s", c.ICSMachine.GroupVersionKind(), c.ICSMachine.Namespace, c.ICSMachine.Name)
}

// Patch updates the object and its status on the API server.
func (c *VIMMachineContext) Patch() error {
	return c.PatchHelper.Patch(c, c.ICSMachine)
}

func (c *VIMMachineContext) GetICSMachine() ICSMachine {
	return c.ICSMachine
}

func (c *VIMMachineContext) GetObjectMeta() v1.ObjectMeta {
	return c.ICSMachine.ObjectMeta
}

func (c *VIMMachineContext) SetBaseMachineContext(base *BaseMachineContext) {
	c.BaseMachineContext = base
}
