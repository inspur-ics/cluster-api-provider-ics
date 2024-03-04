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

package infrastructure

import (
	basetypv1 "github.com/inspur-ics/ics-go-sdk/client/types"
	basevmv1 "github.com/inspur-ics/ics-go-sdk/vm"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
)

type virtualMachineContext struct {
	context.VMContext
	Ref   basetypv1.ManagedObjectReference
	Obj   *basevmv1.VirtualMachineService
	State *infrav1.VirtualMachine
}

func (c *virtualMachineContext) String() string {
	return c.VMContext.String()
}
