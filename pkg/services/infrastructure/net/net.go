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

package net

import (
	"net"
	"strings"

	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"

	"github.com/inspur-ics/ics-go-sdk/client"
	"github.com/inspur-ics/ics-go-sdk/client/types"
	vmapi "github.com/inspur-ics/ics-go-sdk/vm"
	"github.com/pkg/errors"
)

// NetworkStatus provides information about one of a VM's networks.
type NetworkStatus struct {
	// Connected is a flag that indicates whether this network is currently
	// connected to the VM.
	Connected bool `json:"connected,omitempty"`

	// IPAddrs is one or more IP addresses reported by vm-tools.
	// +optional
	IPAddrs []string `json:"ipAddrs,omitempty"`

	// MACAddr is the MAC address of the network device.
	MACAddr string `json:"macAddr"`

	// NetworkName is the name of the network.
	// +optional
	NetworkName string `json:"networkName,omitempty"`
}

// GetNetworkStatus returns the network information for the specified VM.
func GetNetworkStatus(
	ctx *context.VMContext,
	client *client.Client,
	moRef types.ManagedObjectReference) ([]NetworkStatus, error) {

	var (
		obj types.VirtualMachine
	)

	virtualMachineService := vmapi.NewVirtualMachineService(client)
	vm, err := virtualMachineService.GetVM(ctx, moRef.Value)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get vm info, for vm %v", moRef)
	}
	obj = *vm
	if obj.Nics == nil || len(obj.Nics) <= 0 {
		return nil, errors.New("vm nics hardware device is nil")
	}

	var allNetStatus []NetworkStatus

	for _, nic := range obj.Nics {
		if len(nic.ID) != 0 {
			netStatus := NetworkStatus{
				MACAddr: nic.Mac,
			}
			if len(nic.IP) != 0 {
				netStatus.IPAddrs = []string{ nic.IP }
				netStatus.NetworkName = nic.DeviceName
				netStatus.Connected = false
				if strings.Compare("UP", nic.Status) == 0 {
					netStatus.Connected = true
				}
			}
			allNetStatus = append(allNetStatus, netStatus)
		}
	}

	return allNetStatus, nil
}

// ErrOnLocalOnlyIPAddr returns an error if the provided IP address is
// accessible only on the VM's guest OS.
func ErrOnLocalOnlyIPAddr(addr string) error {
	var reason string
	a := net.ParseIP(addr)
	switch {
	case len(a) == 0:
		reason = "invalid"
	case a.IsUnspecified():
		reason = "unspecified"
	case a.IsLinkLocalMulticast():
		reason = "link-local-mutlicast"
	case a.IsLinkLocalUnicast():
		reason = "link-local-unicast"
	case a.IsLoopback():
		reason = "loopback"
	}
	if reason != "" {
		return errors.Errorf("failed to validate ip addr=%v: %s", addr, reason)
	}
	return nil
}