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

package utils

import (
	"fmt"
	"net"
	"os"

	"github.com/projectcalico/libcalico-go/lib/api"
	"github.com/projectcalico/libcalico-go/lib/client"
	cnet "github.com/projectcalico/libcalico-go/lib/net"
	"github.com/vishvananda/netlink"
)

type CalicoClient struct {
	client client.Client
}

func NewCalicoClient(etcdEndpoints string) (*CalicoClient, error) {
	if etcdEndpoints != "" {
		if err := os.Setenv("ETCD_ENDPOINTS", etcdEndpoints); err != nil {
			return nil, err
		}
	}

	// load client config from environment
	clientConfig, err := client.LoadClientConfig("")
	if err != nil {
		return nil, err
	}

	client, err := client.New(*clientConfig)
	if err != nil {
		return nil, err
	}

	return &CalicoClient{client: *client}, nil
}

func (c *CalicoClient) AssignIPv4(podId string) (net.IP, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return net.IP{}, err
	}
	assignArgs := client.AutoAssignArgs{Num4: 1, Num6: 0, HandleID: &podId, Hostname: hostname}
	ipv4, _, err := c.client.IPAM().AutoAssign(assignArgs)
	num4 := len(ipv4)
	if num4 != 1 {
		return net.IP{}, fmt.Errorf("Calico IPAM returned %d IPv4 addresses", num4)
	}

	return ipv4[0].IP, err
}

func (c *CalicoClient) ReleaseByPodId(podId string) error {
	return c.client.IPAM().ReleaseByHandle(podId)
}

func (c *CalicoClient) ConfigureEndpoint(podId string, devName string, ip net.IP) error {
	link, err := netlink.LinkByName(devName)
	if err != nil {
		return err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}

	endpoint := api.NewWorkloadEndpoint()
	endpoint.Metadata.Name = devName
	endpoint.Metadata.Node = hostname

	// TODO: verify if there should be "cni" instead of "k8s" and what this changes
	endpoint.Metadata.Orchestrator = "k8s"

	endpoint.Metadata.Workload = podId

	// TODO: think about setting there pod labels
	// https://github.com/projectcalico/calico-cni/blob/72cc93bc12c6225efc64d584aa877df91aa621bc/k8s/k8s.go#L216
	// we have this in PodSandboxConfig as GetLables()
	endpoint.Metadata.Labels = make(map[string]string)

	// TODO: verify if there should be other data in profiles - now we are using hardcoded network name
	// https://github.com/projectcalico/calico-cni/blob/72cc93bc12c6225efc64d584aa877df91aa621bc/k8s/k8s.go#L110
	endpoint.Spec.Profiles = []string{"virtlet"}

	endpoint.Spec.InterfaceName = devName
	endpoint.Spec.MAC = cnet.MAC{HardwareAddr: link.Attrs().HardwareAddr}

	net := cnet.IPNet{net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}}
	endpoint.Spec.IPNetworks = append(endpoint.Spec.IPNetworks, net)

	if _, err := c.client.WorkloadEndpoints().Apply(endpoint); err != nil {
		return err
	}

	return nil
}

func (c *CalicoClient) RemoveEndpoint(podId string) error {
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}

	if err := c.client.WorkloadEndpoints().Delete(api.WorkloadEndpointMetadata{
		Node:         hostname,
		Orchestrator: "k8s",
		Workload:     podId}); err != nil {

		return err
	}

	return nil
}
