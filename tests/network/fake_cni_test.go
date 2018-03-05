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
	"encoding/json"
	"fmt"
	"log"
	"net"

	"github.com/containernetworking/cni/pkg/ns"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/vishvananda/netlink"

	"github.com/Mirantis/virtlet/pkg/cni"
	"github.com/Mirantis/virtlet/pkg/nettools"
	"github.com/Mirantis/virtlet/pkg/utils"
)

// FakeCNIVethPair represents a veth pair created by the fake CNI
type FakeCNIVethPair struct {
	HostSide netlink.Link
	ContSide netlink.Link
}

type fakeCNIEntry struct {
	podId, podName, podNS   string
	info, infoAfterTeardown *cnicurrent.Result
	extraRoutes             map[int][]netlink.Route
	hostNS, contNS          ns.NetNS
	veths                   []FakeCNIVethPair
	added                   bool
	removed                 bool
	useBadResult            bool
}

func (e *fakeCNIEntry) addSandboxToNetwork(ifaceIndex int) error {
	iface := e.info.Interfaces[ifaceIndex]
	iface.Sandbox = cni.PodNetNSPath(e.podId)

	var err error
	e.contNS, err = ns.GetNS(iface.Sandbox)
	if err != nil {
		return fmt.Errorf("can't get pod netns (path %q): %v", iface.Sandbox, err)
	}

	var vp FakeCNIVethPair
	if err := e.hostNS.Do(func(ns.NetNS) error {
		var err error
		vp.HostSide, vp.ContSide, err = nettools.CreateEscapeVethPair(e.contNS, iface.Name, 1500)
		return err
	}); err != nil {
		return fmt.Errorf("failed to create escape veth pair: %v", err)
	}

	return e.contNS.Do(func(ns.NetNS) error {
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
		if err := nettools.ConfigureLink(vp.ContSide, e.info); err != nil {
			return fmt.Errorf("error configuring link %q: %v", iface.Name, err)
		}
		if e.extraRoutes != nil {
			for _, r := range e.extraRoutes[ifaceIndex] {
				if r.Scope == nettools.SCOPE_LINK {
					r.LinkIndex = vp.ContSide.Attrs().Index
				}
				if err := netlink.RouteAdd(&r); err != nil {
					return fmt.Errorf("Failed to add route %#v: %v", r, err)
				}
			}
		}
		e.veths = append(e.veths, vp)
		e.added = true
		return nil
	})
}

func (c *fakeCNIEntry) captureNetworkConfigAfterTeardown(podId string) error {
	return c.contNS.Do(func(ns.NetNS) error {
		for _, ipConfig := range c.info.IPs {
			ifaceIndex := ipConfig.Interface
			if ifaceIndex > len(c.info.Interfaces) {
				return fmt.Errorf("bad interface index %d", ifaceIndex)
			}
			iface := c.info.Interfaces[ifaceIndex]
			link, err := netlink.LinkByName(iface.Name)
			if err != nil {
				return fmt.Errorf("can't find link %q: %v", iface.Name, err)
			}
			linkInfo, err := nettools.ExtractLinkInfo(link, cni.PodNetNSPath(podId))
			if err != nil {
				return fmt.Errorf("error extracting link info: %v", err)
			}
			if c.infoAfterTeardown == nil {
				c.infoAfterTeardown = linkInfo
			} else {
				if len(linkInfo.Interfaces) != 1 {
					return fmt.Errorf("more than one interface extracted")
				}
				if len(linkInfo.IPs) != 1 {
					return fmt.Errorf("more than one ip config extracted")
				}
				linkInfo.IPs[0].Interface = len(c.infoAfterTeardown.Interfaces)
				c.infoAfterTeardown.IPs = append(c.infoAfterTeardown.IPs, linkInfo.IPs[0])
				c.infoAfterTeardown.Interfaces = append(c.infoAfterTeardown.Interfaces, linkInfo.Interfaces[0])
			}
		}
		return nil
	})
}

func (e *fakeCNIEntry) cleanup() {
	if e.contNS != nil {
		e.contNS.Close()
	}
}

func podKey(podId, podName, podNS string) string {
	return fmt.Sprintf("%s:%s:%s", podId, podName, podNS)
}

// FakeCNIClient fakes a CNI client. It's only good for one-time
// network setup for a single pod network namespace
type FakeCNIClient struct {
	// DummyPodId is an id of dummy pod which is used by the
	// Calico workaround
	DummyPodId string
	entries    map[string]*fakeCNIEntry
}

var _ cni.Client = &FakeCNIClient{}

func NewFakeCNIClient() *FakeCNIClient {
	return &FakeCNIClient{
		DummyPodId: utils.NewUuid(),
		entries:    make(map[string]*fakeCNIEntry),
	}
}

func (c *FakeCNIClient) ExpectPod(podId, podName, podNS string, info *cnicurrent.Result, hostNS ns.NetNS, extraRoutes map[int][]netlink.Route) {
	c.entries[podKey(podId, podName, podNS)] = &fakeCNIEntry{
		podId:       podId,
		podName:     podName,
		podNS:       podNS,
		info:        info,
		hostNS:      hostNS,
		extraRoutes: extraRoutes,
	}
}

func (c *FakeCNIClient) ExpectDummyPod(info *cnicurrent.Result, hostNS ns.NetNS, extraRoutes map[int][]netlink.Route) {
	c.ExpectPod(c.DummyPodId, "", "", info, hostNS, extraRoutes)
}

func (c *FakeCNIClient) GetDummyNetwork() (*cnicurrent.Result, string, error) {
	if err := cni.CreateNetNS(c.DummyPodId); err != nil {
		return nil, "", fmt.Errorf("couldn't create netns for dummy pod %q: %v", c.DummyPodId, err)
	}
	result, err := c.AddSandboxToNetwork(c.DummyPodId, "", "")
	if err != nil {
		return nil, "", err
	}
	return result, cni.PodNetNSPath(c.DummyPodId), nil
}

func (c *FakeCNIClient) getEntry(podId, podName, podNS string) *fakeCNIEntry {
	if entry, found := c.entries[podKey(podId, podName, podNS)]; found {
		return entry
	}
	log.Panicf("Unexpected pod id = %q name = %q ns = %q", podId, podName, podNS)
	return nil
}

func (c *FakeCNIClient) AddSandboxToNetwork(podId, podName, podNS string) (*cnicurrent.Result, error) {
	entry := c.getEntry(podId, podName, podNS)
	if entry.added {
		panic("AddSandboxToNetwork() was already called")
	}

	replaceSandboxPlaceholders(entry.info, podId)
	for n, iface := range entry.info.Interfaces {
		if iface.Sandbox == "" {
			continue
		}

		if err := entry.addSandboxToNetwork(n); err != nil {
			return nil, err
		}
	}

	r := copyCNIResult(entry.info)
	if entry.useBadResult {
		r.Interfaces = nil
		r.Routes = nil
	}
	return r, nil
}

func (c *FakeCNIClient) RemoveSandboxFromNetwork(podId, podName, podNS string) error {
	entry := c.getEntry(podId, podName, podNS)
	if !entry.added {
		panic("RemoveSandboxFromNetwork() was called without prior AddSandboxToNetwork()")
	}
	if entry.removed {
		panic("RemoveSandboxFromNetwork() was already called")
	}

	if err := entry.captureNetworkConfigAfterTeardown(podId); err != nil {
		panic(err)
	}
	entry.removed = true
	return nil
}

func (c *FakeCNIClient) VerifyAdded(podId, podName, podNS string) {
	entry := c.getEntry(podId, podName, podNS)
	if !entry.added {
		panic("Pod sandbox not added to the network")
	}
	if entry.removed {
		panic("Pod sandbox is already removed")
	}
}

func (c *FakeCNIClient) VerifyRemoved(podId, podName, podNS string) {
	entry := c.getEntry(podId, podName, podNS)
	if !entry.added {
		panic("Pod sandbox not added to the network")
	}
	if !entry.removed {
		panic("Pod sandbox not removed from the network")
	}
}

func (c *FakeCNIClient) Cleanup() {
	for _, entry := range c.entries {
		entry.cleanup()
	}
	if _, found := c.entries[podKey(c.DummyPodId, "", "")]; found {
		if err := cni.DestroyNetNS(c.DummyPodId); err != nil {
			log.Panicf("Error destroying dummy pod network ns: %v", err)
		}
	}
}

func (c *FakeCNIClient) Veths(podId, podName, podNS string) []FakeCNIVethPair {
	c.VerifyAdded(podId, podName, podNS)
	return c.getEntry(podId, podName, podNS).veths
}

func (c *FakeCNIClient) NetworkInfoAfterTeardown(podId, podName, podNS string) *cnicurrent.Result {
	c.VerifyRemoved(podId, podName, podNS)
	return c.getEntry(podId, podName, podNS).infoAfterTeardown
}

func (c *FakeCNIClient) UseBadResult(podId, podName, podNS string, useBadResult bool) {
	c.getEntry(podId, podName, podNS).useBadResult = useBadResult
}

func copyCNIResult(result *cnicurrent.Result) *cnicurrent.Result {
	bs, err := json.Marshal(result)
	if err != nil {
		log.Panicf("Error marshalling CNI result: %v", err)
	}
	var newResult *cnicurrent.Result
	if err := json.Unmarshal(bs, &newResult); err != nil {
		log.Panicf("Error unmarshalling CNI result: %v", err)
	}
	return newResult
}

func replaceSandboxPlaceholders(result *cnicurrent.Result, podId string) {
	for _, iface := range result.Interfaces {
		if iface.Sandbox == "placeholder" {
			iface.Sandbox = cni.PodNetNSPath(podId)
		}
	}
}
