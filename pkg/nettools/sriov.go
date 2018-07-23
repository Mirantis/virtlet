/*
Copyright 2018 Mirantis

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

// Some of the code is based on CNI's plugins/main/bridge/bridge.go, pkg/ip/link.go
// Original copyright notice:
//
// Copyright 2014 CNI authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nettools

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/containernetworking/cni/pkg/ns"
	"github.com/golang/glog"
	"github.com/vishvananda/netlink"

	"github.com/Mirantis/virtlet/pkg/network"
	"github.com/Mirantis/virtlet/pkg/utils"
)

// verify if device is pci virtual function (in the same way as does
// that libvirt (src/util/virpci.c:virPCIIsVirtualFunction)
func isSriovVf(link netlink.Link) bool {
	_, err := os.Stat(filepath.Join("/sys/class/net", link.Attrs().Name, "device/physfn"))
	return err == nil
}

func getPCIAddressOfVF(devName string) (string, error) {
	linkDestination, err := os.Readlink(filepath.Join("/sys/class/net", devName, "device"))
	if err != nil {
		return "", err
	}
	if linkDestination[:13] != "../../../0000" {
		return "", fmt.Errorf("unknown address as device symlink: %q", linkDestination)
	}
	// we need pci address without leading "../../../"
	return linkDestination[9:], nil
}

func getDevNameByPCIAddress(address string) (string, error) {
	desiredLinkLocation := "../../../" + address
	devices, err := ioutil.ReadDir("/sys/class/net")
	if err != nil {
		return "", err
	}
	for _, fi := range devices {
		// skip entries in /sys/class/net which are not directories
		// with "device" entry (example: bonding_masters)
		devPath := filepath.Join("/sys/class/net", fi.Name(), "device")
		if _, err := os.Stat(devPath); err != nil {
			continue
		}

		linkDestination, err := os.Readlink(devPath)
		if err != nil {
			return "", err
		}
		if linkDestination == desiredLinkLocation {
			return fi.Name(), nil
		}
	}
	return "", fmt.Errorf("can't find network device with pci address %q", address)
}

func unbindDriverFromDevice(pciAddress string) error {
	return ioutil.WriteFile(
		filepath.Join("/sys/bus/pci/devices", pciAddress, "driver/unbind"),
		[]byte(pciAddress),
		0200,
	)
}

func getDeviceIdentifier(pciAddress string) (string, error) {
	devDir := filepath.Join("/sys/bus/pci/devices", pciAddress)

	vendor, err := ioutil.ReadFile(filepath.Join(devDir, "vendor"))
	if err != nil {
		return "", err
	}

	devID, err := ioutil.ReadFile(filepath.Join(devDir, "device"))
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s %s", vendor, devID), nil
}

func rebindDriverToDevice(pciAddress string) error {
	return ioutil.WriteFile(
		"/sys/bus/pci/drivers_probe",
		[]byte(pciAddress),
		0200,
	)
}

func bindDeviceToVFIO(devIdentifier string) error {
	return ioutil.WriteFile(
		"/sys/bus/pci/drivers/vfio-pci/new_id",
		[]byte(devIdentifier),
		0200,
	)
}

func getVirtFNNo(pciAddress string) (int, error) {
	for i := 0; ; i++ {
		dest, err := os.Readlink(
			filepath.Join("/sys/bus/pci/devices", pciAddress, "physfn",
				fmt.Sprintf("virtfn%d", i),
			),
		)
		if err != nil {
			return 0, err
		}
		if dest[3:] == pciAddress {
			return i, nil
		}
	}
}

func getMasterLinkOfVf(pciAddress string) (netlink.Link, error) {
	dest, err := os.Readlink(filepath.Join("/sys/bus/pci/devices", pciAddress, "physfn"))
	if err != nil {
		return nil, err
	}
	masterDev, err := getDevNameByPCIAddress(dest[3:])
	if err != nil {
		return nil, err
	}
	masterLink, err := netlink.LinkByName(masterDev)
	if err != nil {
		return nil, err
	}

	return masterLink, nil
}

// setMacAndVlanOnVf uses VF pci address to locate its parent device and uses
// it to set mac address and VLAN id on VF.  It needs to be called from the host netns.
func setMacAndVlanOnVf(pciAddress string, mac net.HardwareAddr, vlanID int) error {
	virtFNNo, err := getVirtFNNo(pciAddress)
	if err != nil {
		return fmt.Errorf("cannot find VF number for device with pci address %q: %v", pciAddress, err)
	}
	masterLink, err := getMasterLinkOfVf(pciAddress)
	if err != nil {
		return fmt.Errorf("cannot get link for PF of VF with pci address %q: %v", pciAddress, err)
	}
	if err := netlink.LinkSetVfHardwareAddr(masterLink, virtFNNo, mac); err != nil {
		return fmt.Errorf("cannot set mac address of VF with pci address %q: %v", pciAddress, err)
	}
	err = netlink.LinkSetVfVlan(masterLink, virtFNNo, vlanID)
	if err != nil {
		return fmt.Errorf("cannot set vlan of VF with pci address %q: %v", pciAddress, err)
	}
	return nil
}

func getVfVlanID(pciAddress string) (int, error) {
	virtFNNo, err := getVirtFNNo(pciAddress)
	if err != nil {
		return 0, err
	}
	masterLink, err := getMasterLinkOfVf(pciAddress)
	if err != nil {
		return 0, err
	}

	// vfinfos are gathered using `ip link show` because of failure in vishvananda/netlink
	// which is occuring for bigger netlink queries like one asking for list ov VFs of an interface.
	iplinkOutput, err := exec.Command("ip", "link", "show", masterLink.Attrs().Name).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("error during execution of ip link show: %q\nOutput:%s", err, iplinkOutput)
	}
	vfinfos, err := utils.ParseIPLinkOutput(iplinkOutput)
	if err != nil {
		return 0, fmt.Errorf("error during parsing ip link output for device %q: %v",
			masterLink.Attrs().Name, err)
	}

	for _, vfInfo := range vfinfos {
		if vfInfo.ID == virtFNNo {
			return int(vfInfo.VLanID), nil
		}
	}
	return 0, fmt.Errorf("vlan info for %d vf on %s not found", virtFNNo, masterLink.Attrs().Name)
}

func setupSriovAndGetInterfaceDescription(link netlink.Link, hostNS ns.NetNS) (*network.InterfaceDescription, error) {
	hwAddr := link.Attrs().HardwareAddr
	ifaceName := link.Attrs().Name
	mtu := link.Attrs().MTU
	vlanID := 0

	pciAddress, err := getPCIAddressOfVF(ifaceName)
	if err != nil {
		return nil, err
	}

	// tapmanager protocol needs a file descriptor in Fo field
	// but SR-IOV part is not using it at all, so set it to
	// new file descriptor with /dev/null opened
	fo, err := os.Open("/dev/null")
	if err != nil {
		return nil, err
	}

	// Switch to the host netns to get VLAN ID of the VF using its master device.
	if err := utils.CallInNetNSWithSysfsRemounted(hostNS, func(ns.NetNS) error {
		var err error
		vlanID, err = getVfVlanID(pciAddress)
		return err
	}); err != nil {
		return nil, err
	}

	if err := unbindDriverFromDevice(pciAddress); err != nil {
		return nil, err
	}

	devIdentifier, err := getDeviceIdentifier(pciAddress)
	if err != nil {
		return nil, err
	}

	if err := bindDeviceToVFIO(devIdentifier); err != nil {
		return nil, err
	}

	// Switch to the host netns to set mac address and VLAN id
	// of VF using its master device.
	if err := utils.CallInNetNSWithSysfsRemounted(hostNS, func(ns.NetNS) error {
		return setMacAndVlanOnVf(pciAddress, hwAddr, vlanID)
	}); err != nil {
		return nil, err
	}

	glog.V(3).Infof("Adding interface %q as VF on %s address", ifaceName, pciAddress)

	return &network.InterfaceDescription{
		Type:         network.InterfaceTypeVF,
		Name:         ifaceName,
		Fo:           fo,
		HardwareAddr: hwAddr,
		PCIAddress:   pciAddress,
		MTU:          uint16(mtu),
		VlanID:       vlanID,
	}, nil
}

// ReconstructVFs iterates over stored PCI addresses, rebinding each
// corresponding interface to its host driver, changing its MAC address and name
// to the values stored in csn and then moving it into the container namespace
func ReconstructVFs(csn *network.ContainerSideNetwork, netns ns.NetNS, ignoreUnbind bool) error {
	for _, iface := range csn.Interfaces {
		if iface.Type != network.InterfaceTypeVF {
			continue
		}
		if err := unbindDriverFromDevice(iface.PCIAddress); err != nil {
			if ignoreUnbind != true {
				return err
			}
		}
		if err := rebindDriverToDevice(iface.PCIAddress); err != nil {
			return err
		}
		devName, err := getDevNameByPCIAddress(iface.PCIAddress)
		if err != nil {
			return err
		}
		if err := setMacAndVlanOnVf(iface.PCIAddress, iface.HardwareAddr, iface.VlanID); err != nil {
			return err
		}
		link, err := netlink.LinkByName(devName)
		if err != nil {
			return fmt.Errorf("can't find link with name %q: %v", devName, err)
		}
		tmpName, err := RandomVethName()
		if err != nil {
			return err
		}
		if err := netlink.LinkSetName(link, tmpName); err != nil {
			return fmt.Errorf("can't set random name %q on interface %q: %v", tmpName, iface.Name, err)
		}
		if link, err = netlink.LinkByName(tmpName); err != nil {
			return fmt.Errorf("can't reread link info: %v", err)
		}
		if err := netlink.LinkSetNsFd(link, int(netns.Fd())); err != nil {
			return fmt.Errorf("can't move link %q to netns %q: %v", iface.Name, netns.Path(), err)
		}
		if err := netns.Do(func(ns.NetNS) error {
			if err := netlink.LinkSetName(link, iface.Name); err != nil {
				return fmt.Errorf("can't rename device %q to %q: %v", devName, iface.Name, err)
			}
			return nil
		}); err != nil {
			return err
		}
	}

	return nil
}
