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

package fake

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1alpha3"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"sigs.k8s.io/cluster-api/util/patch"
)

// NewVMContext returns a fake VMContext for unit testing
// reconcilers with a fake client.
func NewVMContext(ctx *context.ControllerContext) *context.VMContext {

	// Create the resources.
	icsVM := newICSVM()

	// Add the resources to the fake client.
	if err := ctx.Client.Create(ctx, &icsVM); err != nil {
		panic(err)
	}

	helper, err := patch.NewHelper(&icsVM, ctx.Client)
	if err != nil {
		panic(err)
	}

	return &context.VMContext{
		ControllerContext: ctx,
		ICSVM:         &icsVM,
		Logger:            ctx.Logger.WithName(icsVM.Name),
		PatchHelper:       helper,
	}
}

func newICSVM() infrav1.ICSVM {
	return infrav1.ICSVM{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Namespace,
			Name:      ICSVMName,
			UID:       types.UID(ICSVMUUID),
		},
		Spec: infrav1.ICSVMSpec{
			VirtualMachineCloneSpec: infrav1.VirtualMachineCloneSpec{
				Server:     "10.10.10.10",
				Datacenter: "dc0",
				Network: infrav1.NetworkSpec{
					Devices: []infrav1.NetworkDeviceSpec{
						{
							NetworkName: "VM Network",
							DHCP4:       true,
							DHCP6:       true,
						},
					},
				},
				NumCPUs:   2,
				MemoryMiB: 2048,
				DiskGiB:   20,
			},
		},
	}
}
