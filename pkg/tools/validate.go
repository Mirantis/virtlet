/*
Copyright 2019 Mirantis

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

package tools

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	expectedCRIProxySocketPath = "/run/criproxy.sock"
	sysCheckNamespace          = "kube-system"
)

type validateCommand struct {
	client KubeClient
	out    io.Writer
}

// NewValidateCommand returns a cobra.Command that validates a cluster readines
// for Virtlet deploy
func NewValidateCommand(client KubeClient, out io.Writer) *cobra.Command {
	v := &validateCommand{client: client, out: out}
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Make sure the cluster is ready for Virtlet deployment",
		Long:  "Check configuration of the cluster nodes to make sure they're ready for Virtlet deployment",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("This command does not accept arguments")
			}
			return v.Run()
		},
	}
	return cmd
}

func (v *validateCommand) Run() error {
	nodeNames, err := v.client.GetNamesOfNodesMarkedForVirtlet()
	if err != nil {
		return err
	}

	if len(nodeNames) == 0 {
		return errors.New("there are no nodes with Virtlet")
	}

	v.info("Nodes with Virtlet: %s", strings.Join(nodeNames, ", "))

	pods, errs := v.prepareSysCheckPods(nodeNames)
	defer v.deleteSysCheckPods(pods)
	for _, errstr := range errs {
		v.info(errstr)
	}

	if len(pods) == 0 {
		return errors.New("couldn't create system check pods on any Virtlet nodes")
	}

	errCount := v.checkCNI(pods)
	errCount += v.checkCRIProxy(pods)
	errCount += v.checkKubeletArgs(pods)

	if errCount != 0 {
		return fmt.Errorf("found %d problems", errCount)
	}
	v.info("Validation successful.")

	return nil
}

func (v *validateCommand) prepareSysCheckPods(nodes []string) (pods []*v1.Pod, errs []string) {
	// TODO: add timeouts
	// TODO: create the pods in parallel
	hostPathType := v1.HostPathDirectory
	var definedPods []*v1.Pod
	for _, name := range nodes {
		v.info("Creating syscheck pod on the node %q", name)
		pod, err := v.client.CreatePod(&v1.Pod{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "virtletsyscheck-" + name,
				Namespace: sysCheckNamespace,
			},
			Spec: v1.PodSpec{
				Volumes: []v1.Volume{
					{
						Name: "hostfs",
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{
								Path: "/",
								Type: &hostPathType,
							},
						},
					},
				},
				Containers: []v1.Container{
					{
						Name:    "syscheck",
						Image:   "busybox",
						Command: []string{"/bin/sh", "-c", "--"},
						Args:    []string{"trap : TERM INT; (while true; do sleep 1000; done) & wait"},
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      "hostfs",
								MountPath: "/mnt",
								ReadOnly:  true,
							},
						},
					},
				},
				NodeSelector: map[string]string{"kubernetes.io/hostname": name},
				HostPID:      true,
			},
		})
		if err != nil {
			errs = append(errs, fmt.Sprintf("SysCheck pod creation failed on the node %q: %v", name, err))
		} else {
			definedPods = append(definedPods, pod)
		}
	}

	var wg sync.WaitGroup
	wg.Add(len(definedPods))
	for _, def := range definedPods {
		go func(podDef *v1.Pod) {
			for {
				// TODO: add a check for container start failure, e.g. when
				// downloading a container image fails
				if pod, err := v.client.GetPod(podDef.Name, sysCheckNamespace); err != nil {
					errs = append(errs, fmt.Sprintf("Status check for SysCheck pod %q failed: %v", podDef.Name, err))
					break
				} else if pod.Status.Phase == v1.PodRunning {
					pods = append(pods, pod)
					break
				}
				time.Sleep(250 * time.Millisecond)
			}
			wg.Done()
		}(def)
	}
	wg.Wait()
	v.info("SysCheck pods on all the Virtlet nodes are running")

	return
}

func (v *validateCommand) info(fmtstring string, a ...interface{}) {
	fmt.Fprintf(v.out, fmtstring+"\n", a...)
}

func (v *validateCommand) deleteSysCheckPods(pods []*v1.Pod) {
	for _, pod := range pods {
		if err := v.client.DeletePod(pod.Name, sysCheckNamespace); err != nil {
			v.info("Error during removal of SysCheck pod %q/%q: %v", sysCheckNamespace, pod.Name, err)
		}
	}
}

func doInAllPods(pods []*v1.Pod, check func(*v1.Pod) int) int {
	// TODO: add timeouts
	var wg sync.WaitGroup
	wg.Add(len(pods))

	errCount := 0
	for _, pod := range pods {
		go func(pod_ *v1.Pod) {
			errCount += check(pod_)
			wg.Done()
		}(pod)
	}

	wg.Wait()
	return errCount
}

func (v *validateCommand) runCheckOnAllNodes(pods []*v1.Pod, description, command string, check func(nodeName, out string) int) int {
	return doInAllPods(pods, func(pod *v1.Pod) int {
		errCount := 0
		var out bytes.Buffer
		_, err := v.client.ExecInContainer(
			pod.Name, "syscheck", pod.Namespace, nil, bufio.NewWriter(&out), nil,
			[]string{
				"/bin/sh", "-c",
				command,
			},
		)
		if err != nil {
			v.info("ERROR: %s verification failed on the node %q: %v", description, pod.Spec.NodeName, err)
			errCount++
		}

		return errCount + check(pod.Spec.NodeName, strings.TrimRight(out.String(), "\r\n"))
	})
}

func (v *validateCommand) checkCNI(pods []*v1.Pod) int {
	// TODO: try to do a CNI setup in a network namespace
	return v.runCheckOnAllNodes(
		pods, "CNI configuration",
		"find /mnt/etc/cni/net.d -name \"*.conf\" -o -name \"*.conflist\" -o -name \"*.json\" | wc -l",
		func(nodeName, out string) int {
			errCount := 0
			if i, err := strconv.Atoi(out); err != nil {
				v.info("ERROR: internal error during conunting CNI configuration files on %q: %v", nodeName, err)
				errCount++
			} else if i == 0 {
				v.info("ERROR: node %q does not have any CNI configuration in /etc/cni/net.d", nodeName)
				errCount++
			}
			return errCount
		},
	)
}

func (v *validateCommand) checkCRIProxy(pods []*v1.Pod) int {
	// TODO: handle custom CRI proxy socket paths
	return v.runCheckOnAllNodes(
		pods, "CRI Proxy",
		"pgrep criproxy | while read pid ; do cat /proc/$pid/cmdline ; done",
		func(nodeName, out string) int {
			errCount := 0
			if len(out) == 0 {
				v.info("ERROR: node %q doesn't have CRI Proxy running", nodeName)
				errCount++
			} else if !strings.Contains(out, expectedCRIProxySocketPath) {
				v.info("ERROR: CRI Proxy doesn't have %q as its socket path on the node %q", expectedCRIProxySocketPath, nodeName)
				errCount++
			}
			return errCount
		},
	)
}

func (v *validateCommand) checkKubeletArgs(pods []*v1.Pod) int {
	// TODO: handle custom CRI proxy socket paths
	return v.runCheckOnAllNodes(
		pods, "kubelet configuration",
		"( pgrep kubelet ; pgrep hyperkube ) | while read pid ; do cat /proc/$pid/cmdline ; done",
		func(nodeName, out string) int {
			errCount := 0
			if len(out) == 0 {
				// FIXME: this may happen if kubelet process has different name
				v.info("ERROR: kubelet process not found on node %q", nodeName)
				errCount++
			} else {
				for _, arg := range []string{
					"--container-runtime=remote",
					"--container-runtime-endpoint=unix:///run/criproxy.sock",
					"--image-service-endpoint=unix:///run/criproxy.sock",
					"--enable-controller-attach-detach=false",
				} {
					if !strings.Contains(out, arg) {
						v.info("kubelet on node %q is missing %q option", nodeName, arg)
						errCount++
					}
				}
			}
			return errCount
		},
	)
}
