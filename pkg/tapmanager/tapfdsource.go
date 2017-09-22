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
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/containernetworking/cni/pkg/ns"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"

	"github.com/Mirantis/virtlet/pkg/cni"
	"github.com/Mirantis/virtlet/pkg/dhcp"
	"github.com/Mirantis/virtlet/pkg/nettools"
)

const (
	calicoNetType = "calico"
)

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

	cniClient    *cni.Client
	dummyGateway net.IP
	fdMap        map[string]*podNetwork
}

var _ FDSource = &TapFDSource{}

// NewTapFDSource returns a TapFDSource for the specified CNI plugin &
// config dir
func NewTapFDSource(cniPluginsDir, cniConfigsDir string) (*TapFDSource, error) {
	cniClient, err := cni.NewClient(cniPluginsDir, cniConfigsDir)
	if err != nil {
		return nil, err
	}

	s := &TapFDSource{
		cniClient: cniClient,
		fdMap:     make(map[string]*podNetwork),
	}

	// Calico needs special treatment here.
	// We need to make network config DHCP-compatible by throwing away
	// Calico's gateway and dev route and using a fake gateway instead.
	// TODO: add better explanation here
	if cniClient.Type() == calicoNetType {
		dummyResult, err := cniClient.GetDummyNetwork()
		if err != nil {
			return nil, err
		}
		if len(dummyResult.IPs) != 1 {
			return nil, fmt.Errorf("expected 1 ip for the dummy network, but got %d", len(dummyResult.IPs))
		}
		s.dummyGateway = dummyResult.IPs[0].Address.IP
	}

	return s, nil
}

// GetFD implements GetFD method of FDSource interface
func (s *TapFDSource) GetFD(key string, data []byte) (int, []byte, error) {
	var payload GetFDPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, nil, fmt.Errorf("error unmarshalling GetFD payload: %v", err)
	}
	pnd := payload.Description

	recover := payload.CNIConfig != nil

	if !recover {
		if err := cni.CreateNetNS(pnd.PodId); err != nil {
			return 0, nil, fmt.Errorf("error creating new netns for pod %s (%s): %v", pnd.PodName, pnd.PodId, err)
		}

		netConfig, err := s.cniClient.AddSandboxToNetwork(pnd.PodId, pnd.PodName, pnd.PodNs)
		if err != nil {
			return 0, nil, fmt.Errorf("error adding pod %s (%s) to CNI network: %v", pnd.PodName, pnd.PodId, err)
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

	// serialize the original config before modifying it
	// (in case if dummyGateway is set)
	respData, err := json.Marshal(netConfig)
	if err != nil {
		return 0, nil, fmt.Errorf("error marshalling net config: %v", err)
	}

	// Calico needs network config to be adjusted for DHCP compatibility
	if s.dummyGateway != nil {
		// TODO: better diagnostics
		if len(netConfig.IPs) != 1 {
			return 0, nil, errors.New("didn't expect more than one IP config")
		}
		if netConfig.IPs[0].Version != "4" {
			return 0, nil, errors.New("IPv4 config was expected")
		}
		// TODO: calculate network mask based on the pod IP and the gateway IP
		netConfig.IPs[0].Address.Mask = net.IPv4Mask(255, 255, 255, 0)
		netConfig.IPs[0].Gateway = s.dummyGateway
		netConfig.Routes = []*cnitypes.Route{
			{
				Dst: net.IPNet{
					IP:   net.IP{0, 0, 0, 0},
					Mask: net.IPMask{0, 0, 0, 0},
				},
				GW: s.dummyGateway,
			},
		}
	}

	netNSPath := cni.PodNetNSPath(pnd.PodId)
	vmNS, err := ns.GetNS(netNSPath)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to open network namespace at %q: %v", netNSPath, err)
	}

	var csn *nettools.ContainerSideNetwork
	var dhcpServer *dhcp.Server
	doneCh := make(chan error)
	if err := vmNS.Do(func(ns.NetNS) error {
		var err error
		if recover {
			csn, err = nettools.RecreateContainerSideNetwork(netConfig)
		} else {
			csn, err = nettools.SetupContainerSideNetwork(netConfig)
		}
		if err != nil {
			return err
		}

		// NOTE: older CNI plugins don't include the hardware address
		// in Result, but it's needed for Cloud-Init based
		// network setup, so we add it here if it's missing
		ensureCNIInterfaceHwAddress(netConfig, csn)

		// TODO: now CNIConfig should always contain interface mac address, so there
		// is no reason to pass it as separate field in dhcp.Config,
		// dhcp.NewServer should need only CNIConfig, instead of dhcp.Config
		// TODO: set up DHCP server for all the interfaces defined in CNIConfig
		dhcpConfg := &dhcp.Config{
			CNIResult:           *csn.Result,
			PeerHardwareAddress: csn.HardwareAddr,
		}
		dhcpServer = dhcp.NewServer(dhcpConfg)
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
		return 0, nil, err
	}

	s.Lock()
	defer s.Unlock()
	s.fdMap[key] = &podNetwork{
		pnd:        *pnd,
		csn:        csn,
		dhcpServer: dhcpServer,
		doneCh:     doneCh,
	}
	return int(csn.TapFile.Fd()), respData, nil
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
	return pn.csn.HardwareAddr, nil
}

func ensureCNIInterfaceHwAddress(netConfig *cnicurrent.Result, csn *nettools.ContainerSideNetwork) {
	// If there's no interface info in netConfig, we can assume that we're dealing
	// with an old-style CNI plugin which only supports a single network interface
	if len(netConfig.Interfaces) > 0 {
		return
	}

	iface := &cnicurrent.Interface{
		Name: "cni0",
		Mac:  csn.HardwareAddr.String(),
	}
	netConfig.Interfaces = []*cnicurrent.Interface{iface}

	for _, IP := range netConfig.IPs {
		IP.Interface = 0
	}
}
