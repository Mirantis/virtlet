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

package network

import (
	"errors"
	"fmt"
	"log"
	"net"

	"github.com/containernetworking/cni/pkg/ns"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/vishvananda/netlink"

	"github.com/Mirantis/virtlet/pkg/cni"
	"github.com/Mirantis/virtlet/pkg/nettools"
)

// FakeCNIVethPair represents a veth pair created by the fake CNI
type FakeCNIVethPair struct {
	HostSide netlink.Link
	ContSide netlink.Link
}

// FakeCNIClient fakes a CNI client. It's only good for one-time
// network setup for a single pod network namespace
type FakeCNIClient struct {
	info                  *cnicurrent.Result
	hostNS, contNS        ns.NetNS
	podId, podName, podNS string
	added                 bool
	removed               bool
	veths                 []FakeCNIVethPair
}

var _ cni.CNIClient = &FakeCNIClient{}

func NewFakeCNIClient(info *cnicurrent.Result, hostNS ns.NetNS, podId, podName, podNS string) *FakeCNIClient {
	return &FakeCNIClient{
		info:    info,
		hostNS:  hostNS,
		podId:   podId,
		podName: podName,
		podNS:   podNS,
	}
}

func (c *FakeCNIClient) GetDummyNetwork() (*cnicurrent.Result, string, error) {
	return nil, "", errors.New("GetDummyNetwork() is not implemented")
}

func (c *FakeCNIClient) verifyPod(podId, podName, podNS string) {
	if podId != c.podId {
		// we use log.Panicf()/panic() because t.Fatalf() from
		// testing will not work inside netns' Do() calls
		log.Panicf("podId mismatch: %q instead of %q", podId, c.podId)
	}
	if podName != c.podName {
		log.Panicf("podName mismatch: %q instead of %q", podId, c.podName)
	}
	if podNS != c.podNS {
		log.Panicf("podNS mismatch: %q instead of %q", podNS, c.podNS)
	}
}

func (c *FakeCNIClient) AddSandboxToNetwork(podId, podName, podNS string) (*cnicurrent.Result, error) {
	c.verifyPod(podId, podName, podNS)
	if c.added {
		panic("AddSandboxToNetwork() was already called")
	}

	for _, iface := range c.info.Interfaces {
		if iface.Sandbox == "" {
			continue
		}
		if iface.Sandbox != "placeholder" {
			log.Panicf("bad sandbox %q: expected empty string or \"placeholder\"", iface.Sandbox)
		}

		nsPath := cni.PodNetNSPath(podId)
		var err error
		c.contNS, err = ns.GetNS(nsPath)
		if err != nil {
			return nil, fmt.Errorf("can't get pod netns (path %q): %v", nsPath, err)
		}
		var vp FakeCNIVethPair
		if err := c.hostNS.Do(func(ns.NetNS) error {
			var err error
			vp.HostSide, vp.ContSide, err = nettools.CreateEscapeVethPair(c.contNS, iface.Name, 1500)
			return err
		}); err != nil {
			return nil, fmt.Errorf("failed to create escape veth pair: %v", err)
		}

		if err := c.contNS.Do(func(ns.NetNS) error {
			hwAddr, err := net.ParseMAC(iface.Mac)
			if err != nil {
				return fmt.Errorf("error parsing hwaddr %q: %v", iface.Mac, err)
			}
			if err := nettools.SetHardwareAddr(vp.ContSide, hwAddr); err != nil {
				return fmt.Errorf("SetHardwareAddr(): %v", err)
			}
			// mac address changed, reload the link
			vp.ContSide, err = netlink.LinkByIndex(vp.ContSide.Attrs().Index)
			if err != nil {
				return fmt.Errorf("can't reload container veth info: %v", err)
			}
			if err := nettools.ConfigureLink(vp.ContSide, c.info); err != nil {
				return fmt.Errorf("error configuring link %q: %v", iface.Name, err)
			}
			c.veths = append(c.veths, vp)
			return nil
		}); err != nil {
			return nil, err
		}
	}

	c.added = true
	return c.info, nil
}

func (c *FakeCNIClient) RemoveSandboxFromNetwork(podId, podName, podNS string) error {
	c.verifyPod(podId, podName, podNS)
	if !c.added {
		panic("RemoveSandboxFromNetwork() was called without prior AddSandboxToNetwork()")
	}
	if c.removed {
		panic("RemoveSandboxFromNetwork() was already called")
	}

	c.removed = true
	return nil
}

func (c *FakeCNIClient) VerifyAdded() {
	if !c.added {
		panic("pod sandbox not added to the network")
	}
	if c.removed {
		panic("pod sandbox is already removed")
	}
}

func (c *FakeCNIClient) VerifyRemoved() {
	if !c.removed {
		panic("pod sandbox not removed from the network")
	}
}

func (c *FakeCNIClient) Cleanup() {
	if c.contNS != nil {
		c.contNS.Close()
	}
}

func (c *FakeCNIClient) Veths() []FakeCNIVethPair {
	c.VerifyAdded()
	return c.veths
}
