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

package net

import (
	"net"
	"strings"

	"github.com/pkg/errors"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	basecltv1 "github.com/inspur-ics/ics-go-sdk/client"
	basetypv1 "github.com/inspur-ics/ics-go-sdk/client/types"
	basetkv1 "github.com/inspur-ics/ics-go-sdk/task"
	basevmv1 "github.com/inspur-ics/ics-go-sdk/vm"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/services/goclient/icenter"
	infrautilv1 "github.com/inspur-ics/cluster-api-provider-ics/pkg/util"
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
	client *basecltv1.Client,
	moRef basetypv1.ManagedObjectReference) ([]NetworkStatus, error) {

	virtualMachineService := basevmv1.NewVirtualMachineService(client)
	vm, err := virtualMachineService.GetVM(ctx, moRef.Value)
	if err != nil {
		ctx.Logger.Error(err, "vm GetNetworkStatus err", "id", moRef)
		return nil, errors.Wrapf(err, "unable to get vm info, for vm %v", moRef)
	}
	if vm.Nics == nil {
		return nil, errors.New("vm nics hardware device is nil")
	}
	// defensive check to ensure we are not removing the UID
	if vm.ID != "" {
		ctx.ICSVM.Spec.UID = vm.ID
	}
	// defensive check to ensure we are not removing the biosUUID
	if vm.UUID != "" {
		ctx.ICSVM.Spec.BiosUUID = vm.UUID
	}

	// set static ip for the second or more nics
	nicDevices := ctx.ICSVM.Spec.Network.Devices
	if len(nicDevices) >= 2 && len(vm.Nics) == len(nicDevices) {
		isStatic := false
		for i := 1; i < len(nicDevices); i++ {
			device := nicDevices[i]
			if device.DHCP4 || device.DHCP6 {
				continue
			}
			nic := vm.Nics[i]
			if nic.StaticIp {
				continue
			}
			isStatic = true
			break
		}
		if isStatic {
			for index := 1; index < len(nicDevices); index++ {
				device := nicDevices[index]
				if device.DHCP4 || device.DHCP6 {
					continue
				}
				nic := &vm.Nics[index]
				if !nic.StaticIp {
					icenter.UpdateNicIPConfig(ctx, nic, &nicDevices[index])
					vm.Nics[index] = *nic
				}
			}
			task, err := virtualMachineService.SetVM(ctx, *vm)
			if err != nil {
				ctx.Logger.Error(err, "failed to set static ips for vm nics", "id", moRef)
			} else {
				// Wait for the VM to be edited.
				taskService := basetkv1.NewTaskService(ctx.Session.Client)
				_, _ = taskService.WaitForResult(ctx, task)
			}
		}
	}

	allNetStatus := []NetworkStatus{}
	for _, nic := range vm.Nics {
		mac := nic.Mac
		ip := nic.IP
		if &mac != nil {
			netStatus := NetworkStatus{
				MACAddr:     nic.Mac,
				NetworkName: nic.DeviceName,
				Connected:   false,
			}
			if &ip != nil && len(ip) != 0 {
				_ = syncIPPool(ctx, nic)
				netStatus.IPAddrs = []string{ip}
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

func syncIPPool(ctx *context.VMContext, nic basetypv1.Nic) error {
	// Get the IPAddress resource for this request.
	ipAddresses := &infrav1.IPAddressList{}
	err := ctx.Client.List(ctx, ipAddresses, ctrlclient.MatchingFields{"metadata.name": nic.IP})
	if err != nil {
		return err
	}
	if ipAddresses.Items == nil {
		_, _ = infrautilv1.CreateOrUpdateIPAddress(ctx, nic.IP, nic)
	}
	return nil
}
