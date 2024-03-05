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

package util

import (
	"net"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterutilv1 "sigs.k8s.io/cluster-api/util"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	basetypv1 "github.com/inspur-ics/ics-go-sdk/client/types"
)

func CreateOrUpdateIPAddress(ctx *context.VMContext, ipAddressName string, network basetypv1.Nic) (runtime.Object, error) {
	// Create or update the IPAddress resource.
	ipAddress := &infrav1.IPAddress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ctx.ICSVM.Namespace,
			Name:      ipAddressName,
		},
	}
	mutateFn := func() (err error) {
		// Ensure the ICSMachine is marked as an owner of the ICSVM.
		ipAddress.SetOwnerReferences(clusterutilv1.EnsureOwnerRef(
			ipAddress.OwnerReferences,
			metav1.OwnerReference{
				APIVersion: ctx.ICSVM.APIVersion,
				Kind:       ctx.ICSVM.Kind,
				Name:       ctx.ICSVM.Name,
				UID:        ctx.ICSVM.UID,
			}))

		// Instruct the IPAddress to use the CAPI bootstrap data resource.
		ipAddress.Spec.VMRef = corev1.ObjectReference{
			APIVersion: ctx.ICSVM.APIVersion,
			Kind:       ctx.ICSVM.Kind,
			Name:       ctx.ICSVM.Name,
			Namespace:  ctx.ICSVM.Namespace,
		}

		ipAddress.Spec.Address = network.IP
		ipAddress.Spec.Gateway = &network.Gateway
		ipAddress.Spec.MACAddr = network.Mac
		return nil
	}
	if _, err := ctrlutil.CreateOrUpdate(ctx, ctx.Client, ipAddress, mutateFn); err != nil {
		if apierrors.IsAlreadyExists(err) {
			ctx.Logger.Info("IPAddress already exists")
			return nil, err
		}
		ctx.Logger.Error(err, "failed to CreateOrUpdate IPAddress",
			"namespace", ipAddress.Namespace, "name", ipAddress.Name)
		return nil, err
	}

	return ipAddress, nil
}

func UpdateNetworkInfo(ctx *context.VMContext, networkStatus []infrav1.NetworkStatus) {
	ctx.ICSVM.Status.Network = networkStatus
	ipAddresses := make([]string, 0, len(networkStatus))
	for _, netStatus := range ctx.ICSVM.Status.Network {
		ipAddresses = append(ipAddresses, netStatus.IPAddrs...)
	}
	ctx.Logger.Info("vm ip addresses", "Addresses", ipAddresses)
	ctx.ICSVM.Status.Addresses = ipAddresses
}

// ConvertIPAddrsToIPs transforms a ipAddrs strings into PreAllocations IP.
func ConvertIPAddrsToPreAllocations(ipAddrs []string) map[string]string {
	if ipAddrs == nil {
		return make(map[string]string)
	}
	ipMap := make(map[string]string)
	for _, addrs := range ipAddrs {
		if strings.Contains(addrs, "/") {
			_, _, err := net.ParseCIDR(addrs)
			if err != nil {
				continue
			}
			ipMap[addrs] = "cidr"
		} else if net.ParseIP(addrs) != nil {
			ipMap[addrs] = "static"
		}
	}
	return ipMap
}

// GetIPFromPools from cluster template
func GetIPFromNetworkConfig(
	ctx *context.VMContext,
	network *infrav1.NetworkDeviceSpec) (*string, *string, error) {
	var ip string
	var netmask string

	if network.IPAddrs == nil {
		return nil, nil, errors.New("invalid network config")
	}

	allocatedIPMap := make(map[string]string)
	allocations := infrav1.IPAddressList{}
	err := ctx.Client.List(ctx, &allocations)
	if err != nil {
		allocations = infrav1.IPAddressList{}
	}
	for _, ipAddress := range allocations.Items {
		allocatedIPMap[ipAddress.Spec.Address] = "unknown"
	}

	preAllocations := ConvertIPAddrsToPreAllocations(network.IPAddrs)

	for ipAddrs, ipType := range preAllocations {
		switch ipType {
		case "cidr":
			{
				preIP, err := GetIPAddressByCIDR(ipAddrs, allocatedIPMap)
				if err != nil {
					continue
				}
				ip = preIP.String()
				netmask = preIP.DefaultMask().String()
			}
		case "static":
			{
				if _, ok := allocatedIPMap[ipAddrs]; ok {
					continue
				}
				ip = ipAddrs
				netmask = net.ParseIP(ipAddrs).DefaultMask().String()
			}
		}
		if &ip != nil {
			break
		}
	}

	return &ip, &netmask, nil
}

// GetIPAddressByCIDR generate ip
func GetIPAddressByCIDR(cidr string, filter map[string]string) (net.IP, error) {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}

	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); addOffsetToIP(ip) {
		if filter != nil {
			if _, ok := filter[ip.String()]; ok {
				continue
			}
		}
		return ip, nil
	}
	return nil, nil
}

// addOffsetToIP computes the value of the IP address with the offset.
func addOffsetToIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}
