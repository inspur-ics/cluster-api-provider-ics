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

//nolint
func TestICSMachineTemplate_ValidateCreate(t *testing.T) {

	g := NewWithT(t)
	tests := []struct {
		name           string
		icsMachine *ICSMachineTemplate
		wantErr        bool
	}{
		{
			name:           "preferredAPIServerCIDR set on creation ",
			icsMachine: createICSMachineTemplate("foo.com", nil, "192.168.0.1/32", []string{}),
			wantErr:        true,
		},
		{
			name:           "ProviderID set on creation",
			icsMachine: createICSMachineTemplate("foo.com", &someProviderID, "", []string{}),
			wantErr:        true,
		},
		{
			name:           "IPs are not in CIDR format",
			icsMachine: createICSMachineTemplate("foo.com", nil, "", []string{"192.168.0.1/32", "192.168.0.3"}),
			wantErr:        true,
		},
		{
			name:           "successful ICSMachine creation",
			icsMachine: createICSMachineTemplate("foo.com", nil, "", []string{"192.168.0.1/32", "192.168.0.3/32"}),
			wantErr:        true,
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
func TestICSMachineTemplate_ValidateUpdate(t *testing.T) {

	g := NewWithT(t)

	tests := []struct {
		name              string
		oldICSMachine *ICSMachineTemplate
		icsMachine    *ICSMachineTemplate
		wantErr           bool
	}{
		{
			name:              "ProviderID cannot be updated",
			oldICSMachine: createICSMachineTemplate("foo.com", nil, "", []string{"192.168.0.1/32"}),
			icsMachine:    createICSMachineTemplate("foo.com", &someProviderID, "", []string{"192.168.0.1/32"}),
			wantErr:           true,
		},
		{
			name:              "updating ips cannot be done",
			oldICSMachine: createICSMachineTemplate("foo.com", nil, "", []string{"192.168.0.1/32"}),
			icsMachine:    createICSMachineTemplate("foo.com", &someProviderID, "", []string{"192.168.0.1/32", "192.168.0.10/32"}),
			wantErr:           true,
		},
		{
			name:              "updating server cannot be done",
			oldICSMachine: createICSMachineTemplate("foo.com", nil, "", []string{"192.168.0.1/32"}),
			icsMachine:    createICSMachineTemplate("baz.com", &someProviderID, "", []string{"192.168.0.1/32", "192.168.0.10/32"}),
			wantErr:           true,
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

func createICSMachineTemplate(server string, providerID *string, preferredAPIServerCIDR string, ips []string) *ICSMachineTemplate {
	ICSMachineTemplate := &ICSMachineTemplate{
		Spec: ICSMachineTemplateSpec{
			Template: ICSMachineTemplateResource{
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
			},
		},
	}
	for _, ip := range ips {
		ICSMachineTemplate.Spec.Template.Spec.Network.Devices = append(ICSMachineTemplate.Spec.Template.Spec.Network.Devices, NetworkDeviceSpec{
			IPAddrs: []string{ip},
		})
	}
	return ICSMachineTemplate
}
