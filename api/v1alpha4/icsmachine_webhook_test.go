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
)

var (
	someProviderID = "ics://42305f0b-dad7-1d3d-5727-0eaffffffffc"
)

//nolint
func TestICSMachine_ValidateCreate(t *testing.T) {

	g := NewWithT(t)
	tests := []struct {
		name       string
		icsMachine *ICSMachine
		wantErr    bool
	}{
		{
			name:       "preferredAPIServerCIDR set on creation ",
			icsMachine: createICSMachine("foo.com", nil, "192.168.0.1/32", []string{}),
			wantErr:    true,
		},
		{
			name:       "IPs are not in CIDR format",
			icsMachine: createICSMachine("foo.com", nil, "", []string{"192.168.0.1/32", "192.168.0.3"}),
			wantErr:    true,
		},
		{
			name:       "successful ICSMachine creation",
			icsMachine: createICSMachine("foo.com", nil, "", []string{"192.168.0.1/32", "192.168.0.3/32"}),
			wantErr:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.icsMachine.ValidateCreate()
			if tc.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

//nolint
func TestICSMachine_ValidateUpdate(t *testing.T) {

	g := NewWithT(t)

	tests := []struct {
		name          string
		oldICSMachine *ICSMachine
		icsMachine    *ICSMachine
		wantErr       bool
	}{
		{
			name:          "ProviderID can be updated",
			oldICSMachine: createICSMachine("foo.com", nil, "", []string{"192.168.0.1/32"}),
			icsMachine:    createICSMachine("foo.com", &someProviderID, "", []string{"192.168.0.1/32"}),
			wantErr:       false,
		},
		{
			name:          "updating ips can be done",
			oldICSMachine: createICSMachine("foo.com", nil, "", []string{"192.168.0.1/32"}),
			icsMachine:    createICSMachine("foo.com", &someProviderID, "", []string{"192.168.0.1/32", "192.168.0.10/32"}),
			wantErr:       false,
		},
		{
			name:          "updating server cannot be done",
			oldICSMachine: createICSMachine("foo.com", nil, "", []string{"192.168.0.1/32"}),
			icsMachine:    createICSMachine("bar.com", &someProviderID, "", []string{"192.168.0.1/32", "192.168.0.10/32"}),
			wantErr:       true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.icsMachine.ValidateUpdate(tc.oldICSMachine)
			if tc.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func createICSMachine(server string, providerID *string, preferredAPIServerCIDR string, ips []string) *ICSMachine {
	ICSMachine := &ICSMachine{
		Spec: ICSMachineSpec{
			ProviderID: providerID,
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
		ICSMachine.Spec.Network.Devices = append(ICSMachine.Spec.Network.Devices, NetworkDeviceSpec{
			IPAddrs: []string{ip},
		})
	}
	return ICSMachine
}
