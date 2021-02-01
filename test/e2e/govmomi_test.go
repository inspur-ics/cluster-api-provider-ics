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

package e2e

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/soap"
)

func init() {
	flag.StringVar(&icsServer, "e2e.icsServer", os.Getenv("ICS_SERVER"), "the ics server used for e2e tests")
	flag.StringVar(&icsDatacenter, "e2e.icsDataceter", os.Getenv("ICS_DATACENTER"), "the inventory path of the ics datacenter in which VMs are created")
	flag.StringVar(&icsFolder, "e2e.icsFolder", os.Getenv("ICS_FOLDER"), "the inventory path of the ics folder in which VMs are created")
	flag.StringVar(&icsPool, "e2e.icsPool", os.Getenv("ICS_RESOURCE_POOL"), "the inventory path of the ics resource pool in which VMs are created")
	flag.StringVar(&icsDatastore, "e2e.icsDatastore", os.Getenv("ICS_DATASTORE"), "the name of the ics datastore in which VMs are created")
	flag.StringVar(&icsNetwork, "e2e.icsNetwork", os.Getenv("ICS_NETWORK"), "the name of the ics network to which VMs are connected")
	flag.StringVar(&icsMachineTemplate, "e2e.icsMachineTemplate", os.Getenv("ICS_MACHINE_TEMPLATE"), "the template from which the Kubernetes VMs are cloned")
	flag.StringVar(&icsHAProxyTemplate, "e2e.icsHAProxyTemplate", os.Getenv("ICS_HAPROXY_TEMPLATE"), "the template from which the HAProxy load balancer VM is cloned")
}

func initICSSession() {
	By("parsing ics server URL")
	serverURL, err := soap.ParseURL(icsServer)
	Expect(err).ShouldNot(HaveOccurred())

	By("creating ics client", func() {
		var err error
		serverURL.User = url.UserPassword(icsUsername, icsPassword)
		icsClient, err = govmomi.NewClient(ctx, serverURL, true)
		Expect(err).ShouldNot(HaveOccurred())
	})

	By("creating ics finder")
	icsFinder = find.NewFinder(icsClient.Client)

	By("configuring ics datacenter")
	datacenter, err := icsFinder.DatacenterOrDefault(ctx, icsDatacenter)
	Expect(err).ShouldNot(HaveOccurred())
	icsFinder.SetDatacenter(datacenter)
}

func destroyVMsWithPrefix(prefix string) {
	vmList, _ := icsFinder.VirtualMachineList(ctx, icsPool)
	for _, vm := range vmList {
		if strings.HasPrefix(vm.Name(), prefix) {
			destroyVM(vm)
		}
	}
}

func destroyVM(vm *object.VirtualMachine) {
	if task, _ := vm.PowerOff(ctx); task != nil {
		if err := task.Wait(ctx); err != nil {
			fmt.Printf("error powering off %s machine: %s\n", vm.Name(), err)
		}
	}
	if task, _ := vm.Destroy(ctx); task != nil {
		if err := task.Wait(ctx); err != nil {
			fmt.Printf("error destroying  %s machine: %s\n", vm.Name(), err)
		}
	}
}
