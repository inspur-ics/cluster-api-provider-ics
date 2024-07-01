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

package icenter

import (
	"math/rand"
	"strings"
	"sync"

	"github.com/google/uuid"
	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/services/goclient/image"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/services/goclient/template"
	"github.com/pkg/errors"

	infrautilv1 "github.com/inspur-ics/cluster-api-provider-ics/pkg/util"
	basetypv1 "github.com/inspur-ics/ics-go-sdk/client/types"
	basehstv1 "github.com/inspur-ics/ics-go-sdk/host"
	basenetv1 "github.com/inspur-ics/ics-go-sdk/network"
	basestv1 "github.com/inspur-ics/ics-go-sdk/storage"
	basevmv1 "github.com/inspur-ics/ics-go-sdk/vm"
)

const (
	// cloud-init metadata config
	METADATA string = `
{
    "hostname": "VM_HOST_NAME",
    "launch_index": 0,
    "name":"VM_HOST_NAME",
    "uuid":"VM_UUID"
}
`
	CLOUDINITTYPE string = "OPENSTACK"

	NormalSwitchType string = "NORMALSWITCH"
	LocalSDNSwitchType string = "SDNSWITCH"
	ExtSDNSwitchType string = "VXLANOPENSTACKSWITCH"
	SDNDeviceType string = "ADVANCEDNETWORK"
	NormalDeviceType string = "NETWORK"
)

var (
	allocatedIPMu sync.Mutex
)

func CreateVM(ctx *context.VMContext, userdata string) error {
	vmType := ctx.ICSVM.Spec.CloneMode
	if vmType == infrav1.ImportVM {
		return ImportVM(ctx, userdata)
	} else {
		return CloneVM(ctx, userdata)
	}
}

// nolint:gocognit
func ImportVM(ctx *context.VMContext, userdata string) error {
	ctx = &context.VMContext{
		ControllerContext: ctx.ControllerContext,
		ICSVM:             ctx.ICSVM,
		Session:           ctx.Session,
		Logger:            ctx.Logger.WithName("icenter"),
		PatchHelper:       ctx.PatchHelper,
	}
	imageName := ctx.ICSVM.Spec.Template
	ovaImage, err := image.FindOvaImageByName(ctx, imageName)
	if err != nil || ovaImage == nil {
		ctx.Logger.Error(err, "failed to find the ova image from ics")
		return errors.Wrapf(err, "unable to get ova image for %q", ctx)
	}

	storageService := basestv1.NewStorageService(ctx.GetSession().Client)
	dataStore, err := storageService.GetStorageInfoByName(ctx, ctx.ICSVM.Spec.Datastore)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the data store from ics")
		return errors.Wrapf(err, "unable to get DataStore for %q", ctx)
	}

	networks := make(map[int]basetypv1.Network)
	networkService := basenetv1.NewNetworkService(ctx.GetSession().Client)
	for index, device := range ctx.ICSVM.Spec.Network.Devices {
		if device.SwitchType == NormalSwitchType || device.SwitchType == LocalSDNSwitchType {
			network, err := networkService.GetNetworkByID(ctx, device.NetworkID)
			if err != nil {
				ctx.Logger.Error(err, "fail to find the network devices from ics")
				return errors.Wrapf(err, "unable to get networks for %q", ctx)
			}
			if device.SwitchType == LocalSDNSwitchType {
				network.ResourceID = device.DeviceID
				network.Name = device.DeviceName
			}
			networks[index] = *network
		} else if device.SwitchType == ExtSDNSwitchType {
			network := basetypv1.Network{
				ID:         device.NetworkID,
				Name:       device.DeviceName,
				ResourceID: device.DeviceID,
				VswitchDto: basetypv1.Switch{
					SwitchType: ExtSDNSwitchType,
				},
			}
			networks[index] = network
		} else {
			ctx.Logger.Error(errors.New("Network Error"), "Failed to config the network switch type by the ICS version")
		}
	}

	host, err := getAvailableHosts(ctx, *dataStore, networks)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the host from ics")
		return errors.Wrapf(err, "unable to get available host for %q", ctx)
	}

	ovaFilePath := ovaImage.Path + "/" + ovaImage.Name
	ovaConfig, err := image.GetVMForm(ctx, ovaFilePath, host.ID, ovaImage.ServerID)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the vm template from ics")
		return errors.Wrapf(err, "unable to get vm template for %q", ctx)
	}
	vmForm := *ovaConfig
	vmForm.UUID = uuid.New().String()   // the vm path /sys/class/dmi/id/product_uuid
	vmForm.Name = ctx.ICSVM.Name
	vmForm.HostID = host.ID
	vmForm.HostName = host.HostName
	vmForm.HostIP = host.Name
	vmForm.DataStoreID = dataStore.ID
	if vmForm.CPUNum % 2 == 0 {
		vmForm.CPUCore = 2
		vmForm.CPUSocket = vmForm.CPUNum / vmForm.CPUCore
	} else {
		vmForm.CPUCore = 1
		vmForm.CPUSocket = vmForm.CPUNum / vmForm.CPUCore
	}

	diskSpecs, err := getOVADisks(dataStore, ovaConfig.Disks)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the disk spec")
		return errors.Wrapf(err, "error getting disk spec for %q", ctx)
	}
	vmForm.Disks = diskSpecs

	networkSpecs, err := getNetworkSpecs(ctx, ovaConfig.Nics, networks)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the network spec")
		return errors.Wrapf(err, "error getting network specs for %q", ctx)
	}
	vmForm.Nics = networkSpecs

	metadata := strings.ReplaceAll(METADATA, "VM_HOST_NAME", vmForm.Name)
	metadata = strings.ReplaceAll(metadata, "VM_UUID", vmForm.UUID)

	vmForm.CloudInit = basetypv1.CloudInit{
		MetaData:       metadata,
		UserData:       userdata,
		DataSourceType: CLOUDINITTYPE,
	}

	if ctx.ICSVM.Spec.User != nil {
		user := ctx.ICSVM.Spec.User
		if user.AuthorizedType == infrav1.PasswordToken {
			vmForm.GuestOSAuthInfo.UserName = user.Name
			vmForm.GuestOSAuthInfo.UserPwd = user.AuthorizedKey
		}
	}

	virtualMachineService := basevmv1.NewVirtualMachineService(ctx.GetSession().Client)
	task, err := virtualMachineService.ImportVM(ctx, vmForm, ovaFilePath, ovaImage.ServerID, 100)
	if err != nil {
		ctx.Logger.Error(err, "failed to import vm by the ova image")
		return errors.Wrapf(err, "error import vm for machine %s", ctx)
	}

	ctx.ICSVM.Status.TaskRef = task.TaskId

	// patch the icsVM early to ensure that the task is
	// reflected in the status right away, this avoid situations
	// of concurrent clones
	if err := ctx.Patch(); err != nil {
		ctx.Logger.Error(err, "patch failed", "icsvm", ctx.ICSVM)
	}

	return nil
}

// Clone kicks off a clone operation on vCenter to create a new virtual machine.
// nolint:gocognit
func CloneVM(ctx *context.VMContext, userdata string) error {
	ctx = &context.VMContext{
		ControllerContext: ctx.ControllerContext,
		ICSVM:             ctx.ICSVM,
		Session:           ctx.Session,
		Logger:            ctx.Logger.WithName("icenter"),
		PatchHelper:       ctx.PatchHelper,
	}

	vmTemplate := basetypv1.VirtualMachine{}
	tpl, err := template.FindTemplate(ctx, ctx.ICSVM.Spec.Template)
	if err != nil || tpl == nil {
		ctx.Logger.Error(err, "fail to find the vm template from ics")
		return errors.Wrapf(err, "unable to get vm template for %q", ctx)
	}
	vmTemplate = *tpl
	vmTemplate.UUID = uuid.New().String()   // the vm path /sys/class/dmi/id/product_uuid
	vmTemplate.Name = ctx.ICSVM.Name

	storageService := basestv1.NewStorageService(ctx.GetSession().Client)
	dataStore, err := storageService.GetStorageInfoByName(ctx, ctx.ICSVM.Spec.Datastore)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the data store from ics")
		return errors.Wrapf(err, "unable to get DataStore for %q", ctx)
	}

	networks := make(map[int]basetypv1.Network)
	networkService := basenetv1.NewNetworkService(ctx.GetSession().Client)
	for index, device := range ctx.ICSVM.Spec.Network.Devices {
		if device.SwitchType == NormalSwitchType || device.SwitchType == LocalSDNSwitchType {
			network, err := networkService.GetNetworkByName(ctx, device.NetworkName)
			if err != nil {
				ctx.Logger.Error(err, "fail to find the network devices from ics")
				return errors.Wrapf(err, "unable to get networks for %q", ctx)
			}
			if device.SwitchType == LocalSDNSwitchType {
				network.ResourceID = device.DeviceID
				network.Name = device.DeviceName
			}
			networks[index] = *network
		} else if device.SwitchType == ExtSDNSwitchType {
			network := basetypv1.Network{
				ID:         device.NetworkID,
				Name:       device.DeviceName,
				ResourceID: device.DeviceID,
				VswitchDto: basetypv1.Switch{
					SwitchType: ExtSDNSwitchType,
				},
			}
			networks[index] = network
		} else {
			ctx.Logger.Error(errors.New("Network Error"), "Failed to config the network switch type by the ICS version")
		}
	}

	host, err := getAvailableHosts(ctx, *dataStore, networks)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the host from ics")
		return errors.Wrapf(err, "unable to get available host for %q", ctx)
	}
	vmTemplate.HostID = host.ID
	vmTemplate.HostName = host.HostName
	vmTemplate.HostIP = host.Name

	// vm cpu config
	cpuNum := ctx.ICSVM.Spec.NumCPUs
	vmTemplate.CPUNum = int(cpuNum)
	if vmTemplate.CPUNum % 2 == 0 {
		vmTemplate.CPUCore = 2
		vmTemplate.CPUSocket = int(cpuNum) / vmTemplate.CPUCore
	} else {
		vmTemplate.CPUCore = 1
		vmTemplate.CPUSocket = int(cpuNum) / vmTemplate.CPUCore
	}

	// vm memory config
	memory := ctx.ICSVM.Spec.MemoryMiB
	vmTemplate.Memory = int(memory)
	vmTemplate.MemoryInByte = vmTemplate.Memory * 1024 * 1024
	vmTemplate.MaxMemory = vmTemplate.Memory
	vmTemplate.MaxMemoryInByte = vmTemplate.MemoryInByte

	if ctx.ICSVM.Spec.User != nil {
		user := ctx.ICSVM.Spec.User
		if user.AuthorizedType == infrav1.PasswordToken {
			vmTemplate.GuestOSAuthInfo.UserName = user.Name
			vmTemplate.GuestOSAuthInfo.UserPwd = user.AuthorizedKey
		}
	}

	diskSpecs, err := getMultiDisks(dataStore, ctx.ICSVM.Spec.Disks, tpl.Disks)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the disk spec")
		return errors.Wrapf(err, "error getting disk spec for %q", ctx)
	}
	vmTemplate.Disks = diskSpecs

	networkSpecs, err := getNetworkSpecs(ctx, tpl.Nics, networks)
	if err != nil {
		ctx.Logger.Error(err, "fail to find the network spec")
		return errors.Wrapf(err, "error getting network specs for %q", ctx)
	}
	vmTemplate.Nics = networkSpecs

	metadata := strings.ReplaceAll(METADATA, "VM_HOST_NAME", vmTemplate.Name)
	metadata = strings.ReplaceAll(metadata, "VM_UUID", vmTemplate.UUID)

	if user := ctx.ICSVM.Spec.User; user != nil {
		authFlag := true
		pwAuth := "\n\nuser: " + user.Name
		if user.AuthorizedType == infrav1.PasswordToken {
			pwAuth = pwAuth + "\npassword: " + user.AuthorizedKey
			pwAuth = pwAuth + "\nchpasswd:\n  expire: false"
		} else if user.AuthorizedType == infrav1.SSHKey {
			pwAuth = pwAuth + "\nssh_authorized_keys:\n- " + user.AuthorizedKey
		} else {
			authFlag = false
		}
		if authFlag {
			userdata = userdata + pwAuth
		}
	}

	vmTemplate.CloudInit = basetypv1.CloudInit{
		MetaData:       metadata,
		UserData:       userdata,
		DataSourceType: CLOUDINITTYPE,
	}

	virtualMachineService := basevmv1.NewVirtualMachineService(ctx.GetSession().Client)
	task, err := virtualMachineService.CreateVMByTemplate(ctx, vmTemplate, true)
	if err != nil {
		ctx.Logger.Error(err, "fail to create vm by the template")
		return errors.Wrapf(err, "error trigging clone op for machine %s", ctx)
	}

	ctx.ICSVM.Status.TaskRef = task.TaskId

	// patch the icsVM early to ensure that the task is
	// reflected in the status right away, this avoid situations
	// of concurrent clones
	if err := ctx.Patch(); err != nil {
		ctx.Logger.Error(err, "patch failed", "icsvm", ctx.ICSVM)
	}
	return nil
}

func getOVADisks(dataStore *basetypv1.Storage,
	devices []basetypv1.Disk) ([]basetypv1.Disk, error) {

	disks := []basetypv1.Disk{}

	for _, disk := range devices {
		disk.Volume.DataStoreID = dataStore.ID
		disk.Volume.DataStoreName = dataStore.Name
		disk.Volume.Format= "RAW"
		disks = append(disks, disk)
	}

	return disks, nil
}

func getMultiDisks(dataStore *basetypv1.Storage,
	specs []infrav1.DiskSpec, devices []basetypv1.Disk) ([]basetypv1.Disk, error) {

	disks := []basetypv1.Disk{}
	sysDisk := devices[0]
	sysDisk.Volume.DataStoreID = dataStore.ID
	sysDisk.Volume.DataStoreName = dataStore.Name
	sysDisk.Volume.DataStoreType = dataStore.DataStoreType
	sysDisk.Volume.Format= "RAW"
	sysDisk.Volume.Size = float64(specs[0].DiskSize)
	sysDisk.Volume.SizeInByte = int(specs[0].DiskSize) * 1024 * 1024 * 1024
	disks = append(disks, sysDisk)
	if len(specs) >= 2 {
		for i := 1; i < len(specs); i++ {
			disk := initDisk()
			disk.Volume.DataStoreID = dataStore.ID
			disk.Volume.DataStoreName = dataStore.Name
			disk.Volume.DataStoreType = dataStore.DataStoreType
			disk.Volume.Size = float64(specs[i].DiskSize)
			disk.Volume.SizeInByte = int(specs[i].DiskSize) * 1024 * 1024 * 1024
			if specs[i].BusModel != "" {
				disk.BusModel = specs[i].BusModel
			}
			if specs[i].VolumePolicy != "" {
				disk.Volume.VolumePolicy = specs[i].VolumePolicy
			}

			disks = append(disks, disk)
		}
	}

	return disks, nil
}

func initDisk() basetypv1.Disk {
	disk := basetypv1.Disk {
		QueueNum: 1,
		BusModel: "VIRTIO",
		EnableNativeIO: false,
		EnableKernelIO: false,
		TotalIops: 0,
		ReadIops: 0,
		WriteIops: 0,
		TotalBps: 0,
		ReadBps: 0,
		WriteBps: 0,
		ReadWriteModel: "NONE",
		Volume: basetypv1.Volume{
			Bootable: false,
			ClusterSize: 262144,
			Format: "RAW",
			Shared: false,
			FormatDisk: false,
			VolumePolicy: "THIN",
		},
	}
	return disk
}

func getNetworkSpecs(ctx *context.VMContext, devices []basetypv1.Nic,
	networks map[int]basetypv1.Network) ([]basetypv1.Nic, error) {

	if len(devices) > len(networks) {
		return nil, errors.Errorf("failed to config network spec for the ICSVM %s/%s", ctx.ICSVM.Namespace, ctx.ICSVM.Name)
	}

	deviceSpecs := []basetypv1.Nic{}
	// Add new NICs based on the machine config.
	for index, nic := range devices {
		if len(ctx.ICSVM.Spec.Network.Devices) > index {
			network := networks[index]
			if network.VswitchDto.SwitchType == LocalSDNSwitchType || network.VswitchDto.SwitchType == ExtSDNSwitchType {
				nic.DeviceID = network.ResourceID
				nic.DeviceName = network.Name
				nic.DeviceType = SDNDeviceType
			} else {
				nic.DeviceID = network.ID
				nic.DeviceName = network.Name
				nic.DeviceType = NormalDeviceType
			}
			nic.NetworkID = network.ID
			nic.SwitchType = network.VswitchDto.SwitchType
			netSpec := ctx.ICSVM.Spec.Network.Devices[index]
			if netSpec.DHCP4 || netSpec.DHCP6 {
				nic.Dhcp = true
				nic.StaticIp = false
			} else {
				if index == 0 {
					UpdateNicIPConfig(ctx, &nic, &netSpec)
				}
			}
		}
		netSpec := nic
		deviceSpecs = append(deviceSpecs, netSpec)
	}
	if len(networks) > len(devices) {
		for index := len(devices); index < len(networks); index++ {
			netSpec := initNic()
			network := networks[index]
			if network.VswitchDto.SwitchType == LocalSDNSwitchType || network.VswitchDto.SwitchType == ExtSDNSwitchType {
				netSpec.DeviceID = network.ResourceID
				netSpec.DeviceName = network.Name
				netSpec.DeviceType = SDNDeviceType
			} else {
				netSpec.DeviceID = network.ID
				netSpec.DeviceName = network.Name
				netSpec.DeviceType = NormalDeviceType
			}
			netSpec.NetworkID = network.ID
			netSpec.SwitchType = network.VswitchDto.SwitchType
			deviceSpec := &ctx.ICSVM.Spec.Network.Devices[index]
			if deviceSpec.DHCP4 || deviceSpec.DHCP6 {
				netSpec.Dhcp = true
			}
			deviceSpecs = append(deviceSpecs, netSpec)
		}
	}

	return deviceSpecs, nil
}

func initNic() basetypv1.Nic {
	nic := basetypv1.Nic {
		QueueLengthSet: false,
		SendQueueLength: 256,
		ReceiveQueueLength: 256,
		Enable: false,
		UplinkRate: 0,
		UplinkBurst: 0,
		DownlinkRate: 0,
		DownlinkBurst: 0,
		Mac: "",
		Model: "VIRTIO",
		Queues: 1,
		AutoGenerated: true,
		PriorityEnabled: false,
		Dhcp: false,
		StaticIp: false,
		UserIp: "",
		Ipv4Netmask: "255.255.255.0",
		Ipv4Gateway: "",
		Ipv4PrimaryDNS: "",
		Ipv4SecondDNS: "",
	}
	return nic
}

func UpdateNicIPConfig(ctx *context.VMContext, netSpec *basetypv1.Nic, deviceSpec *infrav1.NetworkDeviceSpec) {
	// Check to see if the IP is in the list of the device
	// spec's static IP addresses.
	allocatedIPMu.Lock()
	ip, netmask, err := infrautilv1.GetIPFromNetworkConfig(ctx, deviceSpec)
	if err == nil {
		netSpec.Dhcp = false
		netSpec.IP = *ip
		netSpec.Netmask = *netmask
		netSpec.Gateway = deviceSpec.Gateway4
		netSpec.StaticIp = true
		netSpec.UserIp = *ip
		netSpec.Ipv4Netmask = *netmask
		netSpec.Ipv4Gateway = deviceSpec.Gateway4
		_, err := infrautilv1.CreateOrUpdateIPAddress(ctx, *ip, *netSpec)
		if err != nil {
			ctx.Logger.Error(err, "fail to create ipAddress for the icsvm")
		}
	} else {
		ctx.Logger.Error(err, "fail to get ip and netmask for the icsvm")
	}
	allocatedIPMu.Unlock()
}

func getAvailableHosts(ctx *context.VMContext,
	dataStore basetypv1.Storage, networks map[int]basetypv1.Network) (basetypv1.Host, error) {
	var (
		host              = basetypv1.Host{}
		storageHostsIndex = map[string]string{}
		networkHostsIndex = map[string]string{}
		hosts             = []basetypv1.Host{}
		availableHosts      []basetypv1.Host
	)

	hostService := basehstv1.NewHostService(ctx.Session.Client)
	hostList, err := hostService.GetHostList(ctx)
	if err != nil {
		return basetypv1.Host{}, err
	}
	clusterID := ctx.ICSVM.Spec.Cluster
	for _, host := range hostList {
		if host.ID == "" || host.Status != "CONNECTED" {
			continue
		}
		if clusterID != "" && host.ClusterID != clusterID {
			continue
		}
		hosts = append(hosts, host)
	}

	storageHosts, err := hostService.GetHostListByStorageID(ctx, dataStore.ID)
	if err != nil {
		return basetypv1.Host{}, err
	}
	for _, host := range storageHosts {
		if host.ID != "" {
			storageHostsIndex[host.ID] = host.Name
		}
	}

	hostBounds := make(map[string]int)
	for _, network := range networks {
		var  networkHosts []basetypv1.Host
		if network.VswitchDto.SwitchType == NormalSwitchType || network.VswitchDto.SwitchType == LocalSDNSwitchType {
			networkHosts, err = hostService.GetHostListByNetworkID(ctx, network.ID)
			if err != nil {
				return basetypv1.Host{}, err
			}
		} else if network.VswitchDto.SwitchType == ExtSDNSwitchType {
			networkHosts, err = hostService.GetHostListByExtSdnNetworkID(ctx, network.ID)
			if err != nil {
				return basetypv1.Host{}, err
			}
		}
		for _, host := range networkHosts {
			if host.ID != "" {
				hostBounds[host.ID]++
			}
		}
	}
	totalBounds := len(networks)
	for hostID, bounds := range hostBounds {
		if bounds == totalBounds {
			networkHostsIndex[hostID] = "network"
		}
	}

	memoryInByte := int(ctx.ICSVM.Spec.MemoryMiB * 1024 * 1024)
	for _, host := range hosts {
		_, storageOK := storageHostsIndex[host.ID]
		_, networkOK := networkHostsIndex[host.ID]
		if storageOK && networkOK {
			if host.LogicFreeMemoryInByte > memoryInByte {
				availableHosts = append(availableHosts, host)
			}
		}
	}
	if len(availableHosts) > 0 {
		index := rand.Intn(len(availableHosts))
		host = availableHosts[index]
	} else {
		return host, errors.Errorf("No hosts meet the scheduling conditions, selected 0 from the %d hosts", len(hosts))
	}
	return host, nil
}
