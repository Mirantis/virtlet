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

package server

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/golang/glog"
	"go.universe.tf/netboot/dhcp4"

	"github.com/Mirantis/virtlet/pkg/dhcp"
)

type dhcpServer struct {
	mutex         *sync.Mutex
	configuration *dhcp.Configuration
	listener      *dhcp4.Conn
}

func (s *dhcpServer) SetupListener(laddr string) error {
	if listener, err := dhcp4.NewConn(laddr); err != nil {
		return err
	} else {
		s.listener = listener
	}
	return nil
}

func (s *dhcpServer) Close() error {
	return s.listener.Close()
}

func (s *dhcpServer) Serve() error {
	for {
		pkt, intf, err := s.listener.RecvDHCP()
		glog.V(2).Infof("Received dhcp packet from: %s", pkt.HardwareAddr.String())
		if err != nil {
			return fmt.Errorf("receiving DHCP packet: %v", err)
		}
		if intf == nil {
			return fmt.Errorf("received DHCP packet with no interface information - please fill a bug to https://github.com/google/netboot")
		}

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

func (s *dhcpServer) findConfiguration(macAddress []byte) *dhcp.EndpointConfiguration {
	if s.configuration.EndpointConfigurations == nil {
		// we don't have any configurations received
		glog.Warningf("EndpointConfigurations not defined when there was query for HardwareAddr %v", macAddress)
		return nil
	}

	for _, conf := range s.configuration.EndpointConfigurations {
		if bytes.Equal(conf.GetEndpoint().GetHardwareAddress(), macAddress) {
			return conf
		}
	}

	return nil
}

func (s *dhcpServer) prepareResponse(pkt *dhcp4.Packet, serverIP net.IP, mt dhcp4.MessageType) (*dhcp4.Packet, error) {
	conf := s.findConfiguration(pkt.HardwareAddr)
	if conf == nil {
		return nil, fmt.Errorf("can not find configuration for packet from %v", pkt.HardwareAddr)
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

	sAddress := conf.GetIpv4Address()
	ip, ipnet, err := net.ParseCIDR(sAddress)
	if err != nil {
		return nil, fmt.Errorf("configuration for mac %v have malformed ip setting (%s): %v", pkt.HardwareAddr, sAddress, err)
	}
	if ipnet == nil {
		return nil, fmt.Errorf("configuration for mac %v lacks netmask: %s", pkt.HardwareAddr, sAddress)
	}

	p.YourAddr = ip
	// TODO: pass real mask, temporarily set faked on
	// p.Options[dhcp4.OptSubnetMask] = []byte(ipnet.Mask)
	p.Options[dhcp4.OptSubnetMask] = []byte{255, 255, 255, 0}

	defaultRouter := findDefaultRoute(conf, ipnet)
	if defaultRouter != nil {
		p.Options[dhcp4.OptRouters] = []byte(*defaultRouter)
	}
	/* TODO: pass real routes, now we have them broken
	if conf.GetRoutes() != nil {
		// option 121 is for static routes as defined in rfc3442
		if data, err := s.getStaticRoutes(conf, ipnet); err != nil {
			glog.Warningf("Can not transform static routes for mac %v: %v", pkt.HardwareAddr, err)
		} else {
			p.Options[121] = data
		}
	}
	*/

	// 86400 - full 24h
	p.Options[dhcp4.OptLeaseTime] = []byte{0, 1, 81, 128}

	// 43200 - 12h
	p.Options[dhcp4.OptRenewalTime] = []byte{0, 0, 168, 192}

	// 64800 - 18h
	p.Options[dhcp4.OptRebindingTime] = []byte{0, 0, 253, 32}

	// TODO: remove hardcoded DNS
	p.Options[dhcp4.OptDNSServers] = []byte{8, 8, 8, 8}

	return p, nil
}

func (s *dhcpServer) offerDHCP(pkt *dhcp4.Packet, serverIP net.IP) (*dhcp4.Packet, error) {
	return s.prepareResponse(pkt, serverIP, dhcp4.MsgOffer)
}

func (s *dhcpServer) ackDHCP(pkt *dhcp4.Packet, serverIP net.IP) (*dhcp4.Packet, error) {
	return s.prepareResponse(pkt, serverIP, dhcp4.MsgAck)
}

func findDefaultRoute(configuration *dhcp.EndpointConfiguration, network *net.IPNet) *net.IP {
	routes := configuration.GetRoutes()
	if routes == nil {
		return nil
	}
	for _, route := range routes {
		dest := route.GetDestination()
		_, targetNet, err := net.ParseCIDR(dest)
		if err != nil {
			glog.Warningf("Can not parse route destination '%s': %v", dest, err)
			return nil
		}

		if targetNet.String() != "0.0.0.0/0" {
			// not default route
			continue
		}

		router := net.ParseIP(route.GetThrough())
		if !network.Contains(router) {
			// if this default route is not contained in local
			// network - do not pass it there
			// it will be later passed in static routes
			return nil
		}

		// convert known to be ipv4 from ipv6 form
		asBytes := []byte(router)
		v4router := net.IP(asBytes[len(asBytes)-4:])
		return &v4router
	}

	return nil
}

func (s *dhcpServer) getStaticRoutes(configuration *dhcp.EndpointConfiguration, network *net.IPNet) ([]byte, error) {
	var b bytes.Buffer

	// configuration is already tested if it's not nil
	for _, route := range configuration.GetRoutes() {
		dest := route.GetDestination()
		_, targetNet, err := net.ParseCIDR(dest)
		if err != nil {
			return []byte{}, fmt.Errorf("can not parse route destination '%s': %v", dest, err)
		}

		router := net.ParseIP(route.GetThrough())

		if network.Contains(router) && targetNet.String() == "0.0.0.0/0" {
			// already returned as default route
			continue
		}
		b.Write(toDestinationDescriptor(targetNet))

		asBytes := []byte(router)
		b.Write(asBytes[len(asBytes)-4:])
	}

	return b.Bytes(), nil
}

// toDestinationDescriptor returns calculated destination descriptor according to rfc3442 (page 3)
// warning: there is no check if ipnet is in required ipv4 type
func toDestinationDescriptor(network *net.IPNet) []byte {
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
