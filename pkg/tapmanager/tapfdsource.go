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
	"strings"
	"sync"
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
	"github.com/Mirantis/virtlet/pkg/network"
)

const (
	calicoDefaultSubnet = 24
	calicoSubnetVar     = "VIRTLET_CALICO_SUBNET"
)

// InterfaceDescription contains interface type with additional data
// needed to identify it
type InterfaceDescription struct {
	Type         network.InterfaceType `json:"type"`
	HardwareAddr net.HardwareAddr      `json:"mac"`
	FdIndex      int                   `json:"fdIndex"`
	PCIAddress   string                `json:"pciAddress"`
}

// PodNetworkDesc contains the data that are required by TapFDSource
// to set up a tap device for a VM
type PodNetworkDesc struct {
	// PodID specifies the id of the pod
	PodID string `json:"podId"`
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
	// ContainerSideNetwork specifies configuration used to configure retaken
	// environment
	ContainerSideNetwork *network.ContainerSideNetwork `json:"csn"`
}

type podNetwork struct {
	pnd        PodNetworkDesc
	csn        *network.ContainerSideNetwork
	dhcpServer *dhcp.Server
	doneCh     chan error
}

// TapFDSource sets up and tears down Virtlet VM network.
// It implements FDSource interface
type TapFDSource struct {
	sync.Mutex

	cniClient          cni.Client
	dummyNetwork       *cnicurrent.Result
	dummyNetworkNsPath string
	fdMap              map[string]*podNetwork
}

var _ FDSource = &TapFDSource{}

// NewTapFDSource returns a TapFDSource for the specified CNI plugin &
// config dir
func NewTapFDSource(cniClient cni.Client) (*TapFDSource, error) {
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
	if err := cni.CreateNetNS(pnd.PodID); err != nil {
		return nil, nil, fmt.Errorf("error creating new netns for pod %s (%s): %v", pnd.PodName, pnd.PodID, err)
	}

	weHadAnError := false
	podAddedToNetwork := false
	defer func() {
		if weHadAnError {
			if podAddedToNetwork {
				if err := s.cniClient.RemoveSandboxFromNetwork(pnd.PodID, pnd.PodName, pnd.PodNs); err != nil {
					glog.Errorf("Error while emergency removal of pod from cni network due to previous other error: %v", err)
				}
			}
			if err := cni.DestroyNetNS(pnd.PodID); err != nil {
				glog.Errorf("Error while emergency removal of netns: %v", err)
			}
		}
	}()

	netConfig, err := s.cniClient.AddSandboxToNetwork(pnd.PodID, pnd.PodName, pnd.PodNs)
	if err != nil {
		weHadAnError = true
		return nil, nil, fmt.Errorf("error adding pod %s (%s) to CNI network: %v", pnd.PodName, pnd.PodID, err)
	}
	podAddedToNetwork = true
	glog.V(3).Infof("CNI configuration for pod %s (%s): %s", pnd.PodName, pnd.PodID, spew.Sdump(netConfig))

	if netConfig == nil {
		netConfig = &cnicurrent.Result{}
	}

	if payload.Description.DNS != nil {
		netConfig.DNS.Nameservers = pnd.DNS.Nameservers
		netConfig.DNS.Search = pnd.DNS.Search
		netConfig.DNS.Options = pnd.DNS.Options
	}

	var fds []int
	var respData []byte
	var csn *network.ContainerSideNetwork
	if err := s.setupNetNS(key, pnd, func(netNSPath string, allLinks []netlink.Link) (*network.ContainerSideNetwork, error) {
		if netConfig, err = nettools.ValidateAndFixCNIResult(netConfig, netNSPath, allLinks); err != nil {
			weHadAnError = true
			return nil, fmt.Errorf("error fixing cni configuration: %v", err)
		}
		if err := nettools.FixCalicoNetworking(netConfig, s.getDummyNetwork); err != nil {
			// don't fail in this case because there may be even no Calico
			glog.Warningf("Calico detection/fix didn't work: %v", err)
		}
		glog.V(3).Infof("CNI Result after fix:\n%s", spew.Sdump(netConfig))

		var err error
		if csn, err = nettools.SetupContainerSideNetwork(netConfig, netNSPath, allLinks); err != nil {
			return nil, err
		}

		if respData, err = json.Marshal(csn); err != nil {
			return nil, fmt.Errorf("error marshalling net config: %v", err)
		}

		for _, i := range csn.Interfaces {
			fds = append(fds, int(i.Fo.Fd()))
		}
		return csn, nil
	}); err != nil {
		weHadAnError = true
		return nil, nil, err
	}

	for _, iface := range csn.Interfaces {
		if iface.Type == network.InterfaceTypeVF {
			if err := nettools.SetMacOnVf(iface.PCIAddress, iface.HardwareAddr); err != nil {
				weHadAnError = true
				return nil, nil, err
			}
		}
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

	netNSPath := cni.PodNetNSPath(pn.pnd.PodID)

	vmNS, err := ns.GetNS(netNSPath)
	if err != nil {
		return fmt.Errorf("failed to open network namespace at %q: %v", netNSPath, err)
	}

	// Try to be idempotent even if there will be any other error during next functions calls.
	// This can lead to lead to leaking resources in multiple cni plugins case, but unblocks
	// a possibility to RunPodSandbox once again, after failed attempt. Without that - next
	// attempt will fail with info about alredy existing netns so it can not be created.
	defer func() {
		if err := cni.DestroyNetNS(pn.pnd.PodID); err != nil {
			glog.Errorf("Error when removing network namespace for pod sandbox %q: %v", pn.pnd.PodID, err)
		}
	}()

	if err := nettools.ReconstructVFs(pn.csn, vmNS); err != nil {
		return fmt.Errorf("failed to reconstruct SR-IOV devices: %v", err)
	}

	if err := vmNS.Do(func(ns.NetNS) error {
		if err := pn.dhcpServer.Close(); err != nil {
			return fmt.Errorf("failed to stop dhcp server: %v", err)
		}
		<-pn.doneCh
		return nettools.Teardown(pn.csn)
	}); err != nil {
		return err
	}

	if err := s.cniClient.RemoveSandboxFromNetwork(pn.pnd.PodID, pn.pnd.PodName, pn.pnd.PodNs); err != nil {
		return fmt.Errorf("error removing pod sandbox %q from CNI network: %v", pn.pnd.PodID, err)
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

// Stop stops any running DHCP servers associated with TapFDSource
// and closes tap fds without releasing any other resources.
func (s *TapFDSource) Stop() error {
	s.Lock()
	defer s.Unlock()
	var errors []string
	for _, pn := range s.fdMap {
		if err := pn.dhcpServer.Close(); err != nil {
			errors = append(errors, fmt.Sprintf("error stopping dhcp server: %v", err.Error()))
		} else {
			<-pn.doneCh
		}
		for _, i := range pn.csn.Interfaces {
			if err := i.Fo.Close(); err != nil {
				errors = append(errors, fmt.Sprintf("error closing tap fd: %v", err))
			}
		}
	}
	s.fdMap = make(map[string]*podNetwork)
	if errors != nil {
		return fmt.Errorf("Errors while stopping TapFDSource:\n%s", strings.Join(errors, "\n"))
	}
	return nil
}

// Recover recovers the state for the netns after Virtlet restart
func (s *TapFDSource) Recover(key string, data []byte) error {
	var payload GetFDPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("error unmarshalling GetFD payload: %v", err)
	}
	pnd := payload.Description
	csn := payload.ContainerSideNetwork
	if csn == nil {
		return fmt.Errorf("ContainerSideNetwork not passed to Recover()")
	}
	if csn.Result == nil {
		csn.Result = &cnicurrent.Result{}
	}
	return s.setupNetNS(key, pnd, func(netNSPath string, allLinks []netlink.Link) (*network.ContainerSideNetwork, error) {
		if err := nettools.RecoverContainerSideNetwork(csn, netNSPath, allLinks); err != nil {
			return nil, err
		}
		return csn, nil
	})
}

func (s *TapFDSource) setupNetNS(key string, pnd *PodNetworkDesc, initNet func(netNSPath string, allLinks []netlink.Link) (*network.ContainerSideNetwork, error)) error {
	netNSPath := cni.PodNetNSPath(pnd.PodID)
	vmNS, err := ns.GetNS(netNSPath)
	if err != nil {
		return fmt.Errorf("failed to open network namespace at %q: %v", netNSPath, err)
	}

	var csn *network.ContainerSideNetwork
	var dhcpServer *dhcp.Server
	doneCh := make(chan error)
	if err := vmNS.Do(func(ns.NetNS) error {
		// switch /sys to corresponding one in netns
		// to have the correct items under /sys/class/net
		if err := mountSysfs(); err != nil {
			return err
		}
		defer func() {
			if err := unmountSysfs(); err != nil {
				glog.V(3).Infof("Warning, error during umount of /sys: %v", err)
			}
		}()

		allLinks, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("error listing the links: %v", err)
		}

		if csn, err = initNet(netNSPath, allLinks); err != nil {
			return err
		}

		dhcpServer = dhcp.NewServer(csn)
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
		return err
	}

	s.Lock()
	defer s.Unlock()
	s.fdMap[key] = &podNetwork{
		pnd:        *pnd,
		csn:        csn,
		dhcpServer: dhcpServer,
		doneCh:     doneCh,
	}
	return nil
}
