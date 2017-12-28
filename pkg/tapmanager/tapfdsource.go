// +build linux

/*
Copyright 2017 Mirantis

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

package tapmanager

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/containernetworking/cni/pkg/ns"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	"github.com/vishvananda/netlink"

	"github.com/Mirantis/virtlet/pkg/cni"
	"github.com/Mirantis/virtlet/pkg/dhcp"
	"github.com/Mirantis/virtlet/pkg/nettools"
)

const (
	calicoNetType       = "calico"
	calicoDefaultSubnet = 24
	calicoSubnetVar     = "VIRTLET_CALICO_SUBNET"
)

// InterfaceDescription contains interface type with additional data
// needed to identify it
type InterfaceDescription struct {
	Type         nettools.InterfaceType `json:"type"`
	HardwareAddr net.HardwareAddr       `json:"mac"`
	FdIndex      int                    `json:"fdIndex"`
	PCIAddress   string                 `json:"pciAddress"`
}

// PodNetworkDesc contains the data that are required by TapFDSource
// to set up a tap device for a VM
type PodNetworkDesc struct {
	// PodId specifies the id of the pod
	PodId string `json:"podId"`
	// PodNs specifies the namespace of the pod
	PodNs string `json:"podNs"`
	// PodName specifies the name of the pod
	PodName string `json:"podName"`
	// DNS specifies DNS settings for the pod
	DNS *cnitypes.DNS
}

// GetFDPayload contains the data that are required by TapFDSource
// to recover the tap device that was already configured, or create a new one
// if CNIConfig is nil
type GetFDPayload struct {
	// Description specifies pod network description for already
	// prepared network configuration
	Description *PodNetworkDesc `json:"podNetworkDesc"`
	// CNIConfig specifies CNI configuration used to configure retaken
	// environment
	CNIConfig *cnicurrent.Result `json:"cniConfig"`
}

type podNetwork struct {
	pnd        PodNetworkDesc
	csn        *nettools.ContainerSideNetwork
	dhcpServer *dhcp.Server
	doneCh     chan error
}

// TapFDSource sets up and tears down Virtlet VM network.
// It implements FDSource interface
type TapFDSource struct {
	sync.Mutex

	cniClient          cni.CNIClient
	dummyNetwork       *cnicurrent.Result
	dummyNetworkNsPath string
	fdMap              map[string]*podNetwork
}

var _ FDSource = &TapFDSource{}

// NewTapFDSource returns a TapFDSource for the specified CNI plugin &
// config dir
func NewTapFDSource(cniClient cni.CNIClient) (*TapFDSource, error) {
	s := &TapFDSource{
		cniClient: cniClient,
		fdMap:     make(map[string]*podNetwork),
	}

	return s, nil
}

func (s *TapFDSource) getDummyNetwork() (*cnicurrent.Result, string, error) {
	if s.dummyNetwork == nil {
		var err error
		s.dummyNetwork, s.dummyNetworkNsPath, err = s.cniClient.GetDummyNetwork()
		if err != nil {
			return nil, "", err
		}
		// s.dummyGateway = dummyResult.IPs[0].Address.IP

	}
	return s.dummyNetwork, s.dummyNetworkNsPath, nil
}

// GetFDs implements GetFDs method of FDSource interface
func (s *TapFDSource) GetFDs(key string, data []byte) ([]int, []byte, error) {
	var payload GetFDPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, nil, fmt.Errorf("error unmarshalling GetFD payload: %v", err)
	}
	pnd := payload.Description

	recover := payload.CNIConfig != nil

	if !recover {
		if err := cni.CreateNetNS(pnd.PodId); err != nil {
			return nil, nil, fmt.Errorf("error creating new netns for pod %s (%s): %v", pnd.PodName, pnd.PodId, err)
		}

		netConfig, err := s.cniClient.AddSandboxToNetwork(pnd.PodId, pnd.PodName, pnd.PodNs)
		if err != nil {
			return nil, nil, fmt.Errorf("error adding pod %s (%s) to CNI network: %v", pnd.PodName, pnd.PodId, err)
		}
		glog.V(3).Infof("CNI configuration for pod %s (%s): %s", pnd.PodName, pnd.PodId, spew.Sdump(netConfig))

		if payload.Description.DNS != nil {
			netConfig.DNS.Nameservers = pnd.DNS.Nameservers
			netConfig.DNS.Search = pnd.DNS.Search
			netConfig.DNS.Options = pnd.DNS.Options
		}
		payload.CNIConfig = netConfig
	}

	netConfig := payload.CNIConfig

	netNSPath := cni.PodNetNSPath(pnd.PodId)
	vmNS, err := ns.GetNS(netNSPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open network namespace at %q: %v", netNSPath, err)
	}

	var csn *nettools.ContainerSideNetwork
	var dhcpServer *dhcp.Server
	doneCh := make(chan error)
	if err := vmNS.Do(func(ns.NetNS) error {
		// switch /sys to corresponding one in netns
		if err := syscall.Mount("none", "/sys", "sysfs", 0, ""); err != nil {
			return err
		}
		defer func() {
			err := syscall.Unmount("/sys", syscall.MNT_DETACH)
			if err != nil {
				glog.V(3).Infof("Warning, error during umount of /sys: %v", err)
			}
		}()

		if netConfig == nil {
			netConfig = &cnicurrent.Result{}
		}
		allLinks, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("error listing the links: %v", err)
		}
		if netConfig, err = nettools.ValidateAndFixCNIResult(netConfig, netNSPath, allLinks); err != nil {
			return fmt.Errorf("error fixing cni configuration: %v", err)
		}
		if err := nettools.FixCalicoNetworking(netConfig, s.getDummyNetwork); err != nil {
			// don't fail in this case because there may be even no Calico
			glog.Warningf("Calico detection/fix didn't work: %v", err)
		}
		glog.V(3).Infof("CNI Result after fix:\n%s", spew.Sdump(netConfig))

		if recover {
			csn, err = nettools.RecreateContainerSideNetwork(netConfig, netNSPath, allLinks)
		} else {
			csn, err = nettools.SetupContainerSideNetwork(netConfig, netNSPath, allLinks)
		}
		if err != nil {
			return err
		}

		dhcpServer = dhcp.NewServer(csn.Result)
		if err := dhcpServer.SetupListener("0.0.0.0"); err != nil {
			return fmt.Errorf("Failed to set up dhcp listener: %v", err)
		}
		go func() {
			doneCh <- vmNS.Do(func(ns.NetNS) error {
				err := dhcpServer.Serve()
				if err != nil {
					glog.Errorf("dhcp server error: %v", err)
				}
				return err
			})
		}()
		// FIXME: there's some very small possibility for a race here
		// (happens if the VM makes DHCP request before DHCP server is ready)
		// For now, let's make the probability of such problem even smaller
		time.Sleep(500 * time.Millisecond)
		return nil
	}); err != nil {
		return nil, nil, err
	}

	respData, err := json.Marshal(netConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("error marshalling net config: %v", err)
	}

	s.Lock()
	defer s.Unlock()
	s.fdMap[key] = &podNetwork{
		pnd:        *pnd,
		csn:        csn,
		dhcpServer: dhcpServer,
		doneCh:     doneCh,
	}
	var fds []int
	for _, i := range csn.Interfaces {
		fds = append(fds, int(i.Fo.Fd()))
	}
	return fds, respData, nil
}

// Release implements Release method of FDSource interface
func (s *TapFDSource) Release(key string) error {
	s.Lock()
	defer s.Unlock()
	pn, found := s.fdMap[key]
	if !found {
		return fmt.Errorf("bad fd key: %q", key)
	}

	netNSPath := cni.PodNetNSPath(pn.pnd.PodId)

	vmNS, err := ns.GetNS(netNSPath)
	if err != nil {
		return fmt.Errorf("failed to open network namespace at %q: %v", netNSPath, err)
	}

	if err := pn.csn.ReconstructVFs(vmNS); err != nil {
		return fmt.Errorf("failed to reconstruct SR-IOV devices: %v", err)
	}

	if err := vmNS.Do(func(ns.NetNS) error {
		if err := pn.dhcpServer.Close(); err != nil {
			return fmt.Errorf("failed to stop dhcp server: %v", err)
		}
		<-pn.doneCh
		if err := pn.csn.Teardown(); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	if err := s.cniClient.RemoveSandboxFromNetwork(pn.pnd.PodId, pn.pnd.PodName, pn.pnd.PodNs); err != nil {
		return fmt.Errorf("error removing pod sandbox %q from CNI network: %v", pn.pnd.PodId, err)
	}

	if err := cni.DestroyNetNS(pn.pnd.PodId); err != nil {
		return fmt.Errorf("error when removing network namespace for pod sandbox %q: %v", pn.pnd.PodId, err)
	}

	delete(s.fdMap, key)
	return nil
}

// GetInfo implements GetInfo method of FDSource interface
func (s *TapFDSource) GetInfo(key string) ([]byte, error) {
	s.Lock()
	defer s.Unlock()
	pn, found := s.fdMap[key]
	if !found {
		return nil, fmt.Errorf("bad fd key: %q", key)
	}
	var descriptions []InterfaceDescription
	for i, iface := range pn.csn.Interfaces {
		descriptions = append(descriptions, InterfaceDescription{
			FdIndex:      i,
			HardwareAddr: iface.HardwareAddr,
			Type:         iface.Type,
			PCIAddress:   iface.PCIAddress,
		})
	}
	data, err := json.Marshal(descriptions)
	if err != nil {
		return nil, fmt.Errorf("interface descriptions marshaling error: %v", err)
	}
	return data, nil
}
