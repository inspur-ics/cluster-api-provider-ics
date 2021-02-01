/*
Copyright 2019 The Kubernetes Authors.

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
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util/patch"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1alpha3"
)

// MachineContext is a Go context used with a ICSMachine.
type MachineContext struct {
	*ControllerContext
	Cluster        *clusterv1.Cluster
	Machine        *clusterv1.Machine
	ICSCluster     *infrav1.ICSCluster
	ICSMachine     *infrav1.ICSMachine
	Logger         logr.Logger
	PatchHelper    *patch.Helper
}

// String returns ICSMachineGroupVersionKind ICSMachineNamespace/ICSMachineName.
func (c *MachineContext) String() string {
	return fmt.Sprintf("%s %s/%s", c.ICSMachine.GroupVersionKind(), c.ICSMachine.Namespace, c.ICSMachine.Name)
}

// Patch updates the object and its status on the API server.
func (c *MachineContext) Patch() error {
	return c.PatchHelper.Patch(c, c.ICSMachine)
}

// GetLogger returns this context's logger.
func (c *MachineContext) GetLogger() logr.Logger {
	return c.Logger
}
