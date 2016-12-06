/*
Copyright 2016 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

Parts of this file are copied from https://github.com/google/netboot/blob/8e5c0d07937f8c1dea6e5f218b64f6b95c32ada3/pixiecore/dhcp.go

*/

package dhcp

import (
	"bytes"
	"errors"
	"fmt"
	"net"

	"github.com/golang/glog"
	"go.universe.tf/netboot/dhcp4"
)

const (
	serverPort = 67
)

var (
	defaultDNS = []byte{8, 8, 8, 8}
)

type Server struct {
	config   *Config
	listener *dhcp4.Conn
}

func NewServer(config *Config) *Server {
	return &Server{config: config}
}

func (s *Server) SetupListener(laddr string) error {
	if listener, err := dhcp4.NewConn(fmt.Sprintf("%s:%d", laddr, serverPort)); err != nil {
		return err
	} else {
		s.listener = listener
	}
	return nil
}

func (s *Server) Close() error {
	return s.listener.Close()
}

func (s *Server) Serve() error {
	for {
		pkt, intf, err := s.listener.RecvDHCP()
		if err != nil {
			return fmt.Errorf("receiving DHCP packet: %v", err)
		}
		if intf == nil {
			return fmt.Errorf("received DHCP packet with no interface information - please fill a bug to https://github.com/google/netboot")
		}
		glog.V(2).Infof("Received dhcp packet from: %s", pkt.HardwareAddr.String())

		serverIP, err := interfaceIP(intf)
		if err != nil {
			glog.Warningf("Want to respond to %s on %s, but couldn't get a source address: %s", pkt.HardwareAddr.String(), intf.Name, err)
			continue
		}

		var resp *dhcp4.Packet
		switch pkt.Type {
		case dhcp4.MsgDiscover:
			resp, err = s.offerDHCP(pkt, serverIP)
			if err != nil {
				glog.Warningf("Failed to construct DHCP offer for %s: %s", pkt.HardwareAddr.String(), err)
				continue
			}
		case dhcp4.MsgRequest:
			resp, err = s.ackDHCP(pkt, serverIP)
			if err != nil {
				glog.Warningf("Failed to construct DHCP ACK for %s: %s", pkt.HardwareAddr.String(), err)
				continue
			}
		default:
			glog.Warningf("Ignoring packet from %s: packet is %s", pkt.HardwareAddr.String(), pkt.Type.String())
			continue
		}

		if resp != nil {
			glog.V(2).Infof("Sending %s packet to %s", resp.Type.String(), pkt.HardwareAddr.String())
			glog.V(3).Info(resp.DebugString())
			if err = s.listener.SendDHCP(resp, intf); err != nil {
				glog.Warningf("Failed to send DHCP offer for %s: %s", pkt.HardwareAddr.String(), err)
			}
		}
	}
}

func interfaceIP(intf *net.Interface) (net.IP, error) {
	addrs, err := intf.Addrs()
	if err != nil {
		return nil, err
	}

	// Try to find an IPv4 address to use, in the following order:
	// global unicast (includes rfc1918), link-local unicast,
	// loopback.
	fs := [](func(net.IP) bool){
		net.IP.IsGlobalUnicast,
		net.IP.IsLinkLocalUnicast,
		net.IP.IsLoopback,
	}
	for _, f := range fs {
		for _, a := range addrs {
			ipaddr, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipaddr.IP.To4()
			if ip == nil {
				continue
			}
			if f(ip) {
				return ip, nil
			}
		}
	}

	return nil, errors.New("no usable unicast address configured on interface")
}

func (s *Server) prepareResponse(pkt *dhcp4.Packet, serverIP net.IP, mt dhcp4.MessageType) (*dhcp4.Packet, error) {
	if !bytes.Equal(pkt.HardwareAddr, s.config.PeerHardwareAddress) {
		return nil, fmt.Errorf("unexpected packet from %v", pkt.HardwareAddr)
	}

	if s.config.CNIResult.IP4 == nil {
		return nil, fmt.Errorf("IP4 is not specified in CNI config")
	}

	p := &dhcp4.Packet{
		Type:          mt,
		TransactionID: pkt.TransactionID,
		Broadcast:     true,
		HardwareAddr:  pkt.HardwareAddr,
		RelayAddr:     pkt.RelayAddr,
		ServerAddr:    serverIP,
		Options:       make(dhcp4.Options),
	}
	p.Options[dhcp4.OptServerIdentifier] = serverIP

	// if guid was sent, copy it
	if pkt.Options[97] != nil {
		p.Options[97] = pkt.Options[97]
	}

	p.YourAddr = s.config.CNIResult.IP4.IP.IP
	p.Options[dhcp4.OptSubnetMask] = s.config.CNIResult.IP4.IP.Mask

	if s.config.CNIResult.IP4.Gateway != nil {
		p.Options[dhcp4.OptRouters] = []byte(s.config.CNIResult.IP4.Gateway)
	}
	// option 121 is for static routes as defined in rfc3442
	if data, err := s.getStaticRoutes(); err != nil {
		glog.Warningf("Can not transform static routes for mac %v: %v", pkt.HardwareAddr, err)
	} else if data != nil {
		p.Options[121] = data
	}

	// 86400 - full 24h
	p.Options[dhcp4.OptLeaseTime] = []byte{0, 1, 81, 128}

	// 43200 - 12h
	p.Options[dhcp4.OptRenewalTime] = []byte{0, 0, 168, 192}

	// 64800 - 18h
	p.Options[dhcp4.OptRebindingTime] = []byte{0, 0, 253, 32}

	// TODO: include more dns options
	if len(s.config.CNIResult.DNS.Nameservers) == 0 {
		p.Options[dhcp4.OptDNSServers] = defaultDNS
	} else {
		var b bytes.Buffer
		for _, ns := range s.config.CNIResult.DNS.Nameservers {
			ip := net.ParseIP(ns).To4()
			if len(ip) != 4 {
				glog.Warningf("failed to parse nameserver ip %q", ip)
			} else {
				b.Write(ip)
			}
		}
		if b.Len() > 0 {
			p.Options[dhcp4.OptDNSServers] = b.Bytes()
		} else {
			p.Options[dhcp4.OptDNSServers] = defaultDNS
		}
	}

	return p, nil
}

func (s *Server) offerDHCP(pkt *dhcp4.Packet, serverIP net.IP) (*dhcp4.Packet, error) {
	return s.prepareResponse(pkt, serverIP, dhcp4.MsgOffer)
}

func (s *Server) ackDHCP(pkt *dhcp4.Packet, serverIP net.IP) (*dhcp4.Packet, error) {
	return s.prepareResponse(pkt, serverIP, dhcp4.MsgAck)
}

func (s *Server) getStaticRoutes() ([]byte, error) {
	if len(s.config.CNIResult.IP4.Routes) == 0 {
		return nil, nil
	}

	var b bytes.Buffer
	for _, route := range s.config.CNIResult.IP4.Routes {
		b.Write(toDestinationDescriptor(route.Dst))
		if route.GW != nil {
			b.Write(route.GW)
		} else {
			b.Write([]byte{0, 0, 0, 0})
		}
	}

	return b.Bytes(), nil
}

// toDestinationDescriptor returns calculated destination descriptor according to rfc3442 (page 3)
// warning: there is no check if ipnet is in required ipv4 type
func toDestinationDescriptor(network net.IPNet) []byte {
	s, _ := network.Mask.Size()
	ipAsBytes := []byte(network.IP)
	return append(
		[]byte{byte(s)},
		ipAsBytes[:widthOfMaskToSignificantOctets(s)]...,
	)
}

func widthOfMaskToSignificantOctets(mask int) int {
	return (mask + 7) / 8
}
