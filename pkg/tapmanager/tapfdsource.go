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
	"github.com/Mirantis/virtlet/pkg/utils"
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
// to prepare container side network configuration
type GetFDPayload struct {
	// Description contains the pod information and DNS settings for the pod
	Description *PodNetworkDesc `json:"podNetworkDesc"`
}

// RecoverPayload contains the data that are required by TapFDSource
// to recover a network configuration in a pod
type RecoverPayload struct {
	// Description contains the pod information and DNS settings for the pod
	Description *PodNetworkDesc `json:"podNetworkDesc"`
	// ContainerSideNetwork specifies configuration used to configure retaken
	// environment
	ContainerSideNetwork *network.ContainerSideNetwork `json:"csn"`
	// HaveRunningContainers is true if any domains are currently running
	// for this pod. VF reconfiguration is to be skipped if that's the case.
	HaveRunningContainers bool
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
	enableSriov        bool
	calicoSubnetSize   int
}

var _ FDSource = &TapFDSource{}

// NewTapFDSource returns a TapFDSource for the specified CNI plugin &
// config dir
func NewTapFDSource(cniClient cni.Client, enableSriov bool, calicoSubnetSize int) (*TapFDSource, error) {
	s := &TapFDSource{
		cniClient:        cniClient,
		fdMap:            make(map[string]*podNetwork),
		calicoSubnetSize: calicoSubnetSize,
		enableSriov:      enableSriov,
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

	gotError := false
	podAddedToNetwork := false
	defer func() {
		if gotError {
			if podAddedToNetwork {
				if err := s.cniClient.RemoveSandboxFromNetwork(pnd.PodID, pnd.PodName, pnd.PodNs); err != nil {
					glog.Errorf("Error removing a pod from the pod network after failed network setup: %v", err)
				}
			}
			if err := cni.DestroyNetNS(pnd.PodID); err != nil {
				glog.Errorf("Error removing netns after failed network setup: %v", err)
			}
		}
	}()

	netConfig, err := s.cniClient.AddSandboxToNetwork(pnd.PodID, pnd.PodName, pnd.PodNs)
	if err != nil {
		gotError = true
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
	if err := s.setupNetNS(key, pnd, func(netNSPath string, allLinks []netlink.Link, hostNS ns.NetNS) (*network.ContainerSideNetwork, error) {
		if netConfig, err = nettools.ValidateAndFixCNIResult(netConfig, netNSPath, allLinks); err != nil {
			gotError = true
			return nil, fmt.Errorf("error fixing cni configuration: %v", err)
		}
		if err := nettools.FixCalicoNetworking(netConfig, s.calicoSubnetSize, s.getDummyNetwork); err != nil {
			// don't fail in this case because there may be even no Calico
			glog.Warningf("Calico detection/fix didn't work: %v", err)
		}
		glog.V(3).Infof("CNI Result after fix:\n%s", spew.Sdump(netConfig))

		var err error
		if csn, err = nettools.SetupContainerSideNetwork(netConfig, netNSPath, allLinks, s.enableSriov, hostNS); err != nil {
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
		gotError = true
		return nil, nil, err
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

	// Try to keep this function idempotent even if there are errors during the following calls.
	// This can cause some resource leaks in multiple CNI case but makes it possible
	// to call `RunPodSandbox` again after a failed attempt. Failing to do so would cause
	// the next `RunPodSandbox` call to fail due to the netns already being present.
	defer func() {
		if err := cni.DestroyNetNS(pn.pnd.PodID); err != nil {
			glog.Errorf("Error when removing network namespace for pod sandbox %q: %v", pn.pnd.PodID, err)
		}
	}()

	if err := nettools.ReconstructVFs(pn.csn, vmNS, false); err != nil {
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
	var payload RecoverPayload
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
	netNSPath := cni.PodNetNSPath(pnd.PodID)
	vmNS, err := ns.GetNS(netNSPath)
	if err != nil {
		return fmt.Errorf("failed to open network namespace at %q: %v", netNSPath, err)
	}
	if !payload.HaveRunningContainers {
		if err := nettools.ReconstructVFs(csn, vmNS, true); err != nil {
			return err
		}
	}
	return s.setupNetNS(key, pnd, func(netNSPath string, allLinks []netlink.Link, hostNS ns.NetNS) (*network.ContainerSideNetwork, error) {
		if err := nettools.RecoverContainerSideNetwork(csn, netNSPath, allLinks, hostNS); err != nil {
			return nil, err
		}
		return csn, nil
	})
}

// RetrieveFDs retrieves the FDs.
// It's only used in case if VM exited but Recover() didn't populate the FDs
func (s *TapFDSource) RetrieveFDs(key string) ([]int, error) {
	var podNet *podNetwork
	var fds []int
	func() {
		s.Lock()
		defer s.Unlock()
		podNet = s.fdMap[key]
	}()
	if podNet == nil {
		return nil, fmt.Errorf("bad key %q to retrieve FDs", key)
	}

	netNSPath := cni.PodNetNSPath(podNet.pnd.PodID)
	vmNS, err := ns.GetNS(netNSPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open network namespace at %q: %v", netNSPath, err)
	}

	if err := utils.CallInNetNSWithSysfsRemounted(vmNS, func(hostNS ns.NetNS) error {
		allLinks, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("error listing the links: %v", err)
		}

		return nettools.RecoverContainerSideNetwork(podNet.csn, netNSPath, allLinks, hostNS)
	}); err != nil {
		return nil, err
	}

	for _, ifDesc := range podNet.csn.Interfaces {
		// Fail if not all succeeded
		if ifDesc.Fo == nil {
			return nil, fmt.Errorf("failed to open tap interface %q", ifDesc.Name)
		}
		fds = append(fds, int(ifDesc.Fo.Fd()))
	}
	return fds, nil
}

func (s *TapFDSource) setupNetNS(key string, pnd *PodNetworkDesc, initNet func(netNSPath string, allLinks []netlink.Link, hostNS ns.NetNS) (*network.ContainerSideNetwork, error)) error {
	netNSPath := cni.PodNetNSPath(pnd.PodID)
	vmNS, err := ns.GetNS(netNSPath)
	if err != nil {
		return fmt.Errorf("failed to open network namespace at %q: %v", netNSPath, err)
	}

	var csn *network.ContainerSideNetwork
	var dhcpServer *dhcp.Server
	doneCh := make(chan error)
	if err := utils.CallInNetNSWithSysfsRemounted(vmNS, func(hostNS ns.NetNS) error {
		allLinks, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("error listing the links: %v", err)
		}

		if csn, err = initNet(netNSPath, allLinks, hostNS); err != nil {
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
