/*
Copyright 2016-2017 Mirantis

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
	"strings"

	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/golang/glog"
	"go.universe.tf/netboot/dhcp4"
)

const (
	serverPort = 67
	// option 121 is for static routes as defined in rfc3442
	classlessRouteOption = 121
)

var (
	defaultDNS = []byte{8, 8, 8, 8}
)

type Server struct {
	config   *cnicurrent.Result
	listener *dhcp4.Conn
}

func NewServer(config *cnicurrent.Result) *Server {
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

func (s *Server) getInterfaceNo(hwAddr net.HardwareAddr) int {
	addr := hwAddr.String()
	for i, permitted := range s.config.Interfaces {
		if permitted.Mac == addr {
			return i
		}
	}
	return -1
}

func (s *Server) prepareResponse(pkt *dhcp4.Packet, serverIP net.IP, mt dhcp4.MessageType) (*dhcp4.Packet, error) {
	interfaceNo := s.getInterfaceNo(pkt.HardwareAddr)
	if interfaceNo < 0 {
		return nil, fmt.Errorf("unexpected packet from %v", pkt.HardwareAddr)
	}

	var cfg *cnicurrent.IPConfig
	for _, curCfg := range s.config.IPs {
		if curCfg.Version == "4" && curCfg.Interface == interfaceNo {
			cfg = curCfg
		}
	}

	if cfg == nil {
		return nil, fmt.Errorf("IPv4 config for interface %s is not specified in CNI config", pkt.HardwareAddr.String())
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

	p.YourAddr = cfg.Address.IP
	p.Options[dhcp4.OptSubnetMask] = cfg.Address.Mask

	router, routeData, err := s.getStaticRoutes()
	if err != nil {
		glog.Warningf("Can not transform static routes for mac %v: %v", pkt.HardwareAddr, err)
	}
	if router != nil {
		p.Options[dhcp4.OptRouters] = router
	}
	if routeData != nil {
		p.Options[classlessRouteOption] = routeData
	}

	// 86400 - full 24h
	p.Options[dhcp4.OptLeaseTime] = []byte{0, 1, 81, 128}

	// 43200 - 12h
	p.Options[dhcp4.OptRenewalTime] = []byte{0, 0, 168, 192}

	// 64800 - 18h
	p.Options[dhcp4.OptRebindingTime] = []byte{0, 0, 253, 32}

	// TODO: include more dns options
	if len(s.config.DNS.Nameservers) == 0 {
		p.Options[dhcp4.OptDNSServers] = defaultDNS
	} else {
		var b bytes.Buffer
		for _, nsIP := range s.config.DNS.Nameservers {
			ip := net.ParseIP(nsIP).To4()
			if ip == nil {
				glog.Warningf("failed to parse nameserver ip %q", nsIP)
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
	if len(s.config.DNS.Search) != 0 {
		// https://tools.ietf.org/search/rfc3397
		p.Options[119], err = compressedDomainList(s.config.DNS.Search)
		if err != nil {
			return nil, err
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

func (s *Server) getStaticRoutes() (router, routes []byte, err error) {
	if len(s.config.Routes) == 0 {
		return nil, nil, nil
	}

	var b bytes.Buffer
	for _, route := range s.config.Routes {
		if route.Dst.IP == nil {
			return nil, nil, fmt.Errorf("invalid route: %#v", route)
		}
		dstIP := route.Dst.IP.To4()
		gw := route.GW
		if gw == nil {
			// FIXME: this should not be really needed for newer CNI
			var cfg *cnicurrent.IPConfig
			for _, curCfg := range s.config.IPs {
				if curCfg.Version == "4" {
					cfg = curCfg
				}
			}
			gw = cfg.Gateway.To4()
			if gw == nil && cfg.Gateway != nil {
				return nil, nil, fmt.Errorf("unexpected IPv6 gateway address: %#v", gw)
			}
		} else {
			gw = gw.To4()
		}
		if gw != nil && dstIP.Equal(net.IPv4zero) {
			if s, _ := route.Dst.Mask.Size(); s == 0 {
				router = gw
				continue
			}
		}
		b.Write(toDestinationDescriptor(route.Dst))
		if gw != nil {
			b.Write(gw)
		} else {
			b.Write([]byte{0, 0, 0, 0})
		}
	}

	routes = b.Bytes()
	return
}

// toDestinationDescriptor returns calculated destination descriptor according to rfc3442 (page 3)
// warning: there is no check if ipnet is in required ipv4 type
func toDestinationDescriptor(network net.IPNet) []byte {
	s, _ := network.Mask.Size()
	ipAsBytes := []byte(network.IP.To4())
	return append(
		[]byte{byte(s)},
		ipAsBytes[:widthOfMaskToSignificantOctets(s)]...,
	)
}

func widthOfMaskToSignificantOctets(mask int) int {
	return (mask + 7) / 8
}

func compressedDomainList(domainList []string) ([]byte, error) {
	// https://tools.ietf.org/search/rfc1035#section-4.1.4
	// simplified version, only encoding, without real compression
	var b bytes.Buffer
	for n, domain := range domainList {
		// add '\0' between entries
		if n > 0 {
			b.WriteByte(0)
		}

		// encode domain parts as (single byte length)(string)
		parts := strings.Split(domain, ".")
		for _, part := range parts {
			if len(part) > 254 {
				return nil, fmt.Errorf("domain name element '%s' exceeds 254 length limit", part)
			}
			b.WriteByte(byte(len(part)))
			b.WriteString(part)
		}
	}

	return b.Bytes(), nil
}
