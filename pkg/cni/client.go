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
*/

package cni

import (
	"fmt"

	"github.com/containernetworking/cni/libcni"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"

	"github.com/Mirantis/virtlet/pkg/nsfix"
	"github.com/Mirantis/virtlet/pkg/utils"
)

// Client provides an interface to CNI plugins.
type Client interface {
	// AddSandboxToNetwork adds a pod sandbox to the CNI network.
	AddSandboxToNetwork(podID, podName, podNs string) (*cnicurrent.Result, error)
	// RemoveSandboxFromNetwork removes a pod sandbox from the CNI network.
	RemoveSandboxFromNetwork(podID, podName, podNs string) error
	// GetDummyNetwork creates a dummy network using CNI plugin.
	// It's used for making a dummy gateway for Calico CNI plugin.
	// It returns a CNI result and a path to the network namespace.
	GetDummyNetwork() (*cnicurrent.Result, string, error)
}

// client provides an implementation of Client interface.
type client struct {
	pluginsDir string
	configsDir string
}

var _ Client = &client{}

// NewClient returns a client perpared to call plugins in `pluginsDir`
// using configurations found in `configsDir`.
func NewClient(pluginsDir, configsDir string) (*client, error) {
	return &client{
		pluginsDir: pluginsDir,
		configsDir: configsDir,
	}, nil
}

// GetDummyNetwork implements GetDummyNetwork method of Client interface.
func (c *client) GetDummyNetwork() (*cnicurrent.Result, string, error) {
	// TODO: virtlet pod restarts should not grab another address for
	// the gateway. That's not a big problem usually though
	// as the IPs are not returned to Calico so both old
	// IPs on existing VMs and new ones should work.
	podID := utils.NewUUID()
	if err := CreateNetNS(podID); err != nil {
		return nil, "", fmt.Errorf("couldn't create netns for fake pod %q: %v", podID, err)
	}
	r, err := c.AddSandboxToNetwork(podID, "", "")
	if err != nil {
		return nil, "", fmt.Errorf("couldn't set up CNI for fake pod %q: %v", podID, err)
	}
	return r, PodNetNSPath(podID), nil
}

// AddSandboxToNetwork implements AddSandboxToNetwork method of Client interface.
func (c *client) AddSandboxToNetwork(podID, podName, podNs string) (*cnicurrent.Result, error) {
	var r cnicurrent.Result
	if err := nsfix.NewNsFixCall("cniAddSandboxToNetwork").
		Arg(cniRequest{
			PluginsDir: c.pluginsDir,
			ConfigsDir: c.configsDir,
			PodID:      podID,
			PodName:    podName,
			PodNs:      podNs,
		}).
		SpawnInNamespaces(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// RemoveSandboxFromNetwork implements RemoveSandboxFromNetwork method of Client interface.
func (c *client) RemoveSandboxFromNetwork(podID, podName, podNs string) error {
	return nsfix.NewNsFixCall("cniRemoveSandboxFromNetwork").
		Arg(cniRequest{
			PluginsDir: c.pluginsDir,
			ConfigsDir: c.configsDir,
			PodID:      podID,
			PodName:    podName,
			PodNs:      podNs,
		}).
		SpawnInNamespaces(nil)
}

type cniRequest struct {
	PluginsDir string
	ConfigsDir string
	PodID      string
	PodName    string
	PodNs      string
}

type realClient struct {
	cniConfig     *libcni.CNIConfig
	netConfigList *libcni.NetworkConfigList
}

func newRealclient(pluginsDir, configsDir string) (*realClient, error) {
	netConfigList, err := ReadConfiguration(configsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read CNI configuration %q: %v", configsDir, err)
	}
	glog.V(3).Infof("CNI config: name: %q type: %q", netConfigList.Plugins[0].Network.Name, netConfigList.Plugins[0].Network.Type)

	return &realClient{
		cniConfig:     &libcni.CNIConfig{Path: []string{pluginsDir}},
		netConfigList: netConfigList,
	}, nil
}

func (c *realClient) cniRuntimeConf(podID, podName, podNs string) *libcni.RuntimeConf {
	r := &libcni.RuntimeConf{
		ContainerID: podID,
		NetNS:       PodNetNSPath(podID),
		IfName:      "virtlet-eth0",
	}
	if podName != "" && podNs != "" {
		r.Args = [][2]string{
			{"IgnoreUnknown", "1"},
			{"K8S_POD_NAMESPACE", podNs},
			{"K8S_POD_NAME", podName},
			{"K8S_POD_INFRA_CONTAINER_ID", podID},
		}
	}
	return r
}

func handleAddSandboxToNetwork(arg interface{}) (interface{}, error) {
	req := arg.(*cniRequest)
	c, err := newRealclient(req.PluginsDir, req.ConfigsDir)
	if err != nil {
		return nil, err
	}

	rtConf := c.cniRuntimeConf(req.PodID, req.PodName, req.PodNs)
	// NOTE: this annotation is only need by CNI Genie
	rtConf.Args = append(rtConf.Args, [2]string{
		"K8S_ANNOT", `{"cni": "calico"}`,
	})
	glog.V(3).Infof("AddSandboxToNetwork: PodID %q, PodName %q, PodNs %q, runtime config:\n%s",
		req.PodID, req.PodName, req.PodNs, spew.Sdump(rtConf))
	result, err := c.cniConfig.AddNetworkList(c.netConfigList, rtConf)
	if err == nil {
		glog.V(3).Infof("AddSandboxToNetwork: PodID %q, PodName %q, PodNs %q: result:\n%s",
			req.PodID, req.PodName, req.PodNs, spew.Sdump(result))
	} else {
		glog.V(3).Infof("AddSandboxToNetwork: PodID %q, PodName %q, PodNs %q: error: %v",
			req.PodID, req.PodName, req.PodNs, err)
		return nil, err
	}
	r, err := cnicurrent.NewResultFromResult(result)
	if err != nil {
		return nil, fmt.Errorf("error converting CNI result to the current version: %v", err)
	}
	return r, err
}

func handleRemoveSandboxFromNetwork(arg interface{}) (interface{}, error) {
	req := arg.(*cniRequest)
	c, err := newRealclient(req.PluginsDir, req.ConfigsDir)
	if err != nil {
		return nil, err
	}

	glog.V(3).Infof("RemoveSandboxFromNetwork: PodID %q, PodName %q, PodNs %q", req.PodID, req.PodName, req.PodNs)
	err = c.cniConfig.DelNetworkList(c.netConfigList, c.cniRuntimeConf(req.PodID, req.PodName, req.PodNs))
	if err == nil {
		glog.V(3).Infof("RemoveSandboxFromNetwork: PodID %q, PodName %q, PodNs %q: success",
			req.PodID, req.PodName, req.PodNs)
	} else {
		glog.V(3).Infof("RemoveSandboxFromNetwork: PodID %q, PodName %q, PodNs %q: error: %v",
			req.PodID, req.PodName, req.PodNs, err)
	}
	return nil, err
}

func init() {
	nsfix.RegisterNsFixReexec("cniAddSandboxToNetwork", handleAddSandboxToNetwork, cniRequest{})
	nsfix.RegisterNsFixReexec("cniRemoveSandboxFromNetwork", handleRemoveSandboxFromNetwork, cniRequest{})
}
