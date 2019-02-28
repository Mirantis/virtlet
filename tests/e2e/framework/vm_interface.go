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

package framework

import (
	"encoding/xml"
	"flag"
	"fmt"
	"regexp"
	"strconv"
	"time"

	libvirtxml "github.com/libvirt/libvirt-go-xml"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var useDHCPNetworkConfig = flag.Bool("use-dhcp-network-config", false, "use DHCP network config instead of Cloud-Init-based one")

// VMInterface provides API to work with virtlet VM pods
type VMInterface struct {
	controller *Controller
	pod        *PodInterface

	Name string
	PVCs []*PVCInterface
}

// VMOptions defines VM parameters
type VMOptions struct {
	// VM image to use.
	Image string
	// Number of virtual CPUs.
	VCPUCount int
	// SSH public key to add to the VM.
	SSHKey string
	// SSH key source to use
	SSHKeySource string
	// Cloud-init userdata script
	CloudInitScript string
	// Disk driver to use
	DiskDriver string
	// VM resource limit specs
	Limits map[string]string
	// Cloud-init userdata
	UserData string
	// Enable overridding the userdata
	OverwriteUserData bool
	// Replaces cloud-init userdata with a script
	UserDataScript string
	// Data source for the userdata
	UserDataSource string
	// The name of the node to run the VM on
	NodeName string
	// Root volume size spec
	RootVolumeSize string
	// "cni" annotation value for CNI-Genie
	MultiCNI string
	// PVCs (with corresponding PVs) to use
	PVCs []PVCSpec
	// ConfigMap or Secret to inject into the rootfs
	InjectFilesToRootfsFrom string
	// SystemUUID to set
	SystemUUID string
}

func newVMInterface(controller *Controller, name string) *VMInterface {
	return &VMInterface{
		controller: controller,
		Name:       name,
	}
}

// Pod returns ensures that underlying is started and returns it
func (vmi *VMInterface) Pod() (*PodInterface, error) {
	if vmi.pod == nil {
		pod, err := vmi.controller.Pod(vmi.Name, "")
		if err != nil {
			return nil, err
		}
		vmi.pod = pod
	}
	if vmi.pod == nil {
		return nil, fmt.Errorf("pod %s in namespace %s cannot be found", vmi.Name, vmi.controller.namespace.Name)
	}
	if vmi.pod.Pod.Status.Phase != v1.PodRunning {
		err := vmi.pod.Wait()
		if err != nil {
			return nil, err
		}
	}
	return vmi.pod, nil
}

// PodWithoutChecks returns the underlying pods without performing any
// checks
func (vmi *VMInterface) PodWithoutChecks() *PodInterface {
	return vmi.pod
}

// Create creates a new VM pod
func (vmi *VMInterface) Create(options VMOptions, beforeCreate func(*PodInterface)) error {
	pod := newPodInterface(vmi.controller, vmi.buildVMPod(options))
	for _, pvcSpec := range options.PVCs {
		pvc := newPersistentVolumeClaim(vmi.controller, pvcSpec)
		if err := pvc.Create(); err != nil {
			return err
		}
		pvc.AddToPod(pod, pvcSpec.Name)
		vmi.PVCs = append(vmi.PVCs, pvc)
	}
	if beforeCreate != nil {
		beforeCreate(pod)
	}
	if err := pod.Create(); err != nil {
		return err
	}
	vmi.pod = pod
	return nil
}

// CreateAndWait creates a new VM pod in k8s and waits for it to start
func (vmi *VMInterface) CreateAndWait(options VMOptions, waitTimeout time.Duration, beforeCreate func(*PodInterface)) error {
	err := vmi.Create(options, beforeCreate)
	if err == nil {
		err = vmi.pod.Wait(waitTimeout)
	}
	return err
}

// Delete deletes VM pod and waits for it to disappear from k8s
func (vmi *VMInterface) Delete(waitTimeout time.Duration) error {
	if vmi.pod == nil {
		return nil
	}
	if err := vmi.pod.Delete(); err != nil {
		return err
	}
	if err := vmi.pod.WaitForDestruction(waitTimeout); err != nil {
		return err
	}
	for _, pvc := range vmi.PVCs {
		if err := pvc.Delete(); err != nil {
			return err
		}
		if err := pvc.WaitForDestruction(); err != nil {
			return err
		}
	}
	return nil
}

// VirtletPod returns pod in which virtlet instance, responsible for this VM is located
// (i.e. kube-system:virtlet-xxx pod on the same node)
func (vmi *VMInterface) VirtletPod() (*PodInterface, error) {
	vmPod, err := vmi.Pod()
	if err != nil {
		return nil, err
	}

	node := vmPod.Pod.Spec.NodeName
	pod, err := vmi.controller.FindPod("kube-system", map[string]string{"runtime": "virtlet"},
		func(pod *PodInterface) bool {
			return pod.Pod.Spec.NodeName == node
		},
	)
	if err != nil {
		return nil, err
	} else if pod == nil {
		return nil, fmt.Errorf("cannot find virtlet pod on node %s", node)
	}
	return pod, nil
}

func (vmi *VMInterface) buildVMPod(options VMOptions) *v1.Pod {
	annotations := map[string]string{
		"kubernetes.io/target-runtime":      "virtlet.cloud",
		"VirtletDiskDriver":                 options.DiskDriver,
		"VirtletCloudInitUserDataOverwrite": strconv.FormatBool(options.OverwriteUserData),
	}
	if *useDHCPNetworkConfig {
		annotations["VirtletForceDHCPNetworkConfig"] = "true"
	}

	if options.SSHKey != "" {
		annotations["VirtletSSHKeys"] = options.SSHKey
	}
	if options.SSHKeySource != "" {
		annotations["VirtletSSHKeySource"] = options.SSHKeySource
	}
	if options.UserData != "" {
		annotations["VirtletCloudInitUserData"] = options.UserData
	}
	if options.UserDataSource != "" {
		annotations["VirtletCloudInitUserDataSource"] = options.UserDataSource
	}
	if options.UserDataScript != "" {
		annotations["VirtletCloudInitUserDataScript"] = options.UserDataScript
	}
	if options.VCPUCount > 0 {
		annotations["VirtletVCPUCount"] = strconv.Itoa(options.VCPUCount)
	}
	if options.RootVolumeSize != "" {
		annotations["VirtletRootVolumeSize"] = options.RootVolumeSize
	}
	if options.MultiCNI != "" {
		annotations["cni"] = options.MultiCNI
	}
	if options.InjectFilesToRootfsFrom != "" {
		annotations["VirtletFilesFromDataSource"] = options.InjectFilesToRootfsFrom
	}
	if options.SystemUUID != "" {
		annotations["VirtletSystemUUID"] = options.SystemUUID
	}

	limits := v1.ResourceList{}
	for k, v := range options.Limits {
		limits[v1.ResourceName(k)] = resource.MustParse(v)
	}

	var nodeMatch v1.NodeSelectorRequirement
	if options.NodeName == "" {
		nodeMatch = v1.NodeSelectorRequirement{
			Key:      "extraRuntime",
			Operator: "In",
			Values:   []string{"virtlet"},
		}
	} else {
		nodeMatch = v1.NodeSelectorRequirement{
			Key:      "kubernetes.io/hostname",
			Operator: "In",
			Values:   []string{options.NodeName},
		}
	}

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        vmi.Name,
			Namespace:   vmi.controller.namespace.Name,
			Annotations: annotations,
		},
		Spec: v1.PodSpec{
			Affinity: &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{
							{
								MatchExpressions: []v1.NodeSelectorRequirement{
									nodeMatch,
								},
							},
						},
					},
				},
			},
			Containers: []v1.Container{
				{
					Name:  vmi.Name,
					Image: "virtlet.cloud/" + options.Image,
					Resources: v1.ResourceRequirements{
						Limits: limits,
					},
					ImagePullPolicy: v1.PullIfNotPresent,
					Stdin:           true,
					TTY:             true,
				},
			},
		},
	}
}

// SSH returns SSH executor that can run commands in VM
func (vmi *VMInterface) SSH(user, secret string) (Executor, error) {
	return newSSHInterface(vmi, user, secret)
}

// DomainName returns libvirt domain name the VM
func (vmi *VMInterface) DomainName() (string, error) {
	pod, err := vmi.Pod()
	if err != nil {
		return "", err
	}
	if len(pod.Pod.Status.ContainerStatuses) != 1 {
		return "", fmt.Errorf("expected single container status, but got %d statuses", len(pod.Pod.Status.ContainerStatuses))
	}
	containerID := pod.Pod.Status.ContainerStatuses[0].ContainerID
	match := regexp.MustCompile("__(.+)$").FindStringSubmatch(containerID)
	if len(match) < 2 {
		return "", fmt.Errorf("invalid container ID %q", containerID)
	}
	return fmt.Sprintf("virtlet-%s-%s", match[1][:13], pod.Pod.Status.ContainerStatuses[0].Name), nil
}

// VirshCommand runs virsh command in the virtlet pod, responsible for this VM
// Domain name is automatically substituted into commandline in place of `<domain>`
func (vmi *VMInterface) VirshCommand(command ...string) (string, error) {
	virtletPod, err := vmi.VirtletPod()
	if err != nil {
		return "", err
	}
	for i, c := range command {
		switch c {
		case "<domain>":
			domainName, err := vmi.DomainName()
			if err != nil {
				return "", err
			}
			command[i] = domainName
		}
	}
	return RunVirsh(virtletPod, command...)
}

// Domain returns libvirt domain definition for the VM
func (vmi *VMInterface) Domain() (libvirtxml.Domain, error) {
	domainXML, err := vmi.VirshCommand("dumpxml", "<domain>")
	if err != nil {
		return libvirtxml.Domain{}, err
	}
	var domain libvirtxml.Domain
	err = xml.Unmarshal([]byte(domainXML), &domain)
	return domain, err
}

// RunVirsh runs virsh command in the given virtlet pod
func RunVirsh(virtletPod *PodInterface, command ...string) (string, error) {
	container, err := virtletPod.Container("virtlet")
	if err != nil {
		return "", err
	}
	cmd := append([]string{"virsh"}, command...)
	return RunSimple(container, cmd...)
}
