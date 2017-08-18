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
	"fmt"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"
)

type VMInterface struct {
	controller *Controller
	pod        *PodInterface

	Name string
}

type VMOptions struct {
	Image           string
	VCPUCount       int
	SSHKey          string
	CloudInitScript string
	DiskDriver      string
	Limits          map[string]string
}

func newVMInterface(controller *Controller, name string) *VMInterface {
	return &VMInterface{
		controller: controller,
		Name:       name,
	}
}

func (vmi *VMInterface) Pod() (*PodInterface, error) {
	if vmi.pod == nil {
		pod, err := vmi.controller.Pod(vmi.Name, "")
		if err != nil {
			return nil, err
		}
		vmi.pod = pod
	}
	if vmi.pod == nil {
		return nil, fmt.Errorf("pod %s in namespace %s cannot be found", vmi.Name, vmi.controller.Namespace.Name)
	}
	if vmi.pod.Pod.Status.Phase != v1.PodRunning {
		err := vmi.pod.Wait()
		if err != nil {
			return nil, err
		}
	}
	return vmi.pod, nil
}

func (vmi *VMInterface) Create(options VMOptions, waitTimeout time.Duration) error {
	pod := newPodInterface(vmi.controller, vmi.buildVMPod(options))
	err := pod.Create()
	if err != nil {
		return err
	}
	err = pod.Wait(waitTimeout)
	if err != nil {
		return err
	}
	vmi.pod = pod
	return nil
}

func (vmi *VMInterface) findVirtletPod() (*PodInterface, error) {
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
		"kubernetes.io/target-runtime":   "virtlet",
		"VirtletDiskDriver":              options.DiskDriver,
		"VirtletSSHKeys":                 options.SSHKey,
		"VirtletCloudInitUserDataScript": options.SSHKey,
	}
	if options.VCPUCount > 0 {
		annotations["VirtletVCPUCount"] = strconv.Itoa(options.VCPUCount)
	}

	limits := v1.ResourceList{}
	for k, v := range options.Limits {
		limits[v1.ResourceName(k)] = resource.MustParse(v)
	}

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        vmi.Name,
			Namespace:   vmi.controller.Namespace.Name,
			Annotations: annotations,
		},
		Spec: v1.PodSpec{
			Affinity: &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{
							{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{
										Key:      "extraRuntime",
										Operator: "In",
										Values:   []string{"virtlet"},
									},
								},
							},
						},
					},
				},
			},
			Containers: []v1.Container{
				{
					Name:  vmi.Name,
					Image: "virtlet/" + options.Image,
					Resources: v1.ResourceRequirements{
						Limits: limits,
					},
				},
			},
		},
	}
}

func (vmi *VMInterface) SSH(user, secret string) (*SSHInterface, error) {
	return newSSHInterface(vmi, user, secret)
}
