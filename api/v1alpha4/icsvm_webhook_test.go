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

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var (
	biosUUID = "ics://42305f0b-dad7-1d3d-5727-0eafffffbbbfc"
)

//nolint
func TestICSVM_ValidateCreate(t *testing.T) {

	g := NewWithT(t)
	tests := []struct {
		name      string
		icsVM *ICSVM
		wantErr   bool
	}{
		{
			name:      "preferredAPIServerCIDR set on creation ",
			icsVM: createICSVM("foo.com", "", "192.168.0.1/32", []string{}, nil),
			wantErr:   true,
		},
		{
			name:      "IPs are not in CIDR format",
			icsVM: createICSVM("foo.com", "", "", []string{"192.168.0.1/32", "192.168.0.3"}, nil),
			wantErr:   true,
		},
		{
			name:      "successful ICSVM creation",
			icsVM: createICSVM("foo.com", "", "", []string{"192.168.0.1/32", "192.168.0.3/32"}, nil),
			wantErr:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.icsVM.ValidateCreate()
			if tc.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

//nolint
func TestICSVM_ValidateUpdate(t *testing.T) {

	g := NewWithT(t)

	tests := []struct {
		name         string
		oldICSVM *ICSVM
		icsVM    *ICSVM
		wantErr      bool
	}{
		{
			name:         "ProviderID can be updated",
			oldICSVM: createICSVM("foo.com", "", "", []string{"192.168.0.1/32"}, nil),
			icsVM:    createICSVM("foo.com", biosUUID, "", []string{"192.168.0.1/32"}, nil),
			wantErr:      false,
		},
		{
			name:         "updating ips can be done",
			oldICSVM: createICSVM("foo.com", "", "", []string{"192.168.0.1/32"}, nil),
			icsVM:    createICSVM("foo.com", biosUUID, "", []string{"192.168.0.1/32", "192.168.0.10/32"}, nil),
			wantErr:      false,
		},
		{
			name:         "updating bootstrapRef can be done",
			oldICSVM: createICSVM("foo.com", "", "", []string{"192.168.0.1/32"}, nil),
			icsVM:    createICSVM("foo.com", biosUUID, "", []string{"192.168.0.1/32", "192.168.0.10/32"}, &corev1.ObjectReference{}),
			wantErr:      false,
		},
		{
			name:         "updating server cannot be done",
			oldICSVM: createICSVM("foo.com", "", "", []string{"192.168.0.1/32"}, nil),
			icsVM:    createICSVM("bar.com", biosUUID, "", []string{"192.168.0.1/32", "192.168.0.10/32"}, nil),
			wantErr:      true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.icsVM.ValidateUpdate(tc.oldICSVM)
			if tc.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func createICSVM(server string, biosUUID string, preferredAPIServerCIDR string, ips []string, bootstrapRef *corev1.ObjectReference) *ICSVM {
	ICSVM := &ICSVM{
		Spec: ICSVMSpec{
			BiosUUID:     biosUUID,
			BootstrapRef: bootstrapRef,
			VirtualMachineCloneSpec: VirtualMachineCloneSpec{
				Server: server,
				Network: NetworkSpec{
					PreferredAPIServerCIDR: preferredAPIServerCIDR,
					Devices:                []NetworkDeviceSpec{},
				},
			},
		},
	}
	for _, ip := range ips {
		ICSVM.Spec.Network.Devices = append(ICSVM.Spec.Network.Devices, NetworkDeviceSpec{
			IPAddrs: []string{ip},
		})
	}
	return ICSVM
}
