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
	"errors"
	"fmt"
	"net"

	"github.com/golang/glog"
	"go.universe.tf/netboot/dhcp4"
)

type dHCPServer struct {
	moduleServer
	listener *dhcp4.Conn
}

func (s dHCPServer) SetupListener(family, laddr string) error {
	var err error
	s.listener, err = dhcp4.NewConn(laddr)
	return err
}

func (s dHCPServer) Close() error {
	return s.listener.Close()
}

func (s dHCPServer) Serve() error {
	for {
		pkt, intf, err := s.listener.RecvDHCP()
		if err != nil {
			return fmt.Errorf("receiving DHCP packet: %v", err)
		}
		if intf == nil {
			return fmt.Errorf("received DHCP packet with no interface information - please fill a bug to https://github.com/google/netboot")
		}

		serverIP, err := interfaceIP(intf)
		if err != nil {
			glog.Warningf("Want to respond to %s on %s, but couldn't get a source address: %s", pkt.HardwareAddr, intf.Name, err)
			continue
		}

		var resp *dhcp4.Packet
		switch pkt.Type {
		case dhcp4.MsgDiscover:
			resp, err = s.offerDHCP(pkt, serverIP)
			if err != nil {
				glog.Warningf("Failed to construct DHCP offer for %s: %s", pkt.HardwareAddr, err)
				continue
			}
		case dhcp4.MsgRequest:
			resp, err = s.ackDHCP(pkt, serverIP)
			if err != nil {
				glog.Warningf("Failed to construct DHCP ACK for %s: %s", pkt.HardwareAddr, err)
				continue
			}
		default:
			glog.Warningf("Ignoring packet from %s: packet is %s, not %s", pkt.HardwareAddr, pkt.Type, dhcp4.MsgDiscover)
			continue
		}

		if err = s.listener.SendDHCP(resp, intf); err != nil {
			glog.Warningf("Failed to send DHCP offer for %s: %s", pkt.HardwareAddr, err)
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

func (s *dHCPServer) offerDHCP(pkt *dhcp4.Packet, serverIP net.IP) (*dhcp4.Packet, error) {
	p := &dhcp4.Packet{
		Type:          dhcp4.MsgOffer,
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

	// TODO: add there rest of options, with ip address/dns/routes and so on

	return p, nil
}

func (s *dHCPServer) ackDHCP(pkt *dhcp4.Packet, serverIP net.IP) (*dhcp4.Packet, error) {
	p := &dhcp4.Packet{
		Type:          dhcp4.MsgAck,
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

	// TODO: add there rest of options, with ip address/dns/routes and so on

	return p, nil
}
