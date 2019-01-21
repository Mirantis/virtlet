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
	defaultCRIProxySockLocation = "/run/criproxy.sock"
	sysCheckNamespace           = "kube-system"
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
		Short: "Validate cluster readiness for Virtlet deployment",
		Long:  "Check configuration of cluster nodes valiating their readiness for Virtlet deployment",
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
	nodes, err := v.client.GetVirtletNodeNames()
	if err != nil {
		return err
	}

	if len(nodes) == 0 {
		return errors.New("There are no nodes with label extraRuntime=virtlet")
	}

	v.info("Nodes labeled with extraRuntime=virtlet: %s", strings.Join(nodes, ", "))

	pods, errs := v.prepareSysCheckPods(nodes)
	defer v.deleteSysCheckPods(pods)
	for _, errstr := range errs {
		v.info(errstr)
	}

	if len(pods) == 0 {
		return errors.New("Could not create system check pods on any Virtlet node")
	}

	errsNumber := v.checkCNI(pods)
	errsNumber += v.checkCRIProxy(pods)
	errsNumber += v.checkKubeletArgs(pods)

	if errsNumber != 0 {
		return fmt.Errorf("Collected %d errors while running SysCheck pods", errsNumber)
	} else {
		v.info("No errors found with")
	}

	return nil
}

func (v *validateCommand) prepareSysCheckPods(nodes []string) (pods []*v1.Pod, errs []string) {
	// TODO: this whole part should be running in a timeouted context
	// TODO: paralelize pods creation
	hostPathType := v1.HostPathDirectory
	var definedPods []*v1.Pod
	for _, name := range nodes {
		v.info("Creating syscheck pod on node %q", name)
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
			errs = append(errs, fmt.Sprintf("SysCheck pod creation failed on node %q: %v", name, err))
		} else {
			definedPods = append(definedPods, pod)
		}
	}

	var wg sync.WaitGroup
	wg.Add(len(definedPods))
	for _, def := range definedPods {
		go func() {
			for {
				// TODO: add checking for possible container starting failure, e.g. when there was an error while
				// downloading container image
				if pod, err := v.client.GetPod(def.Name, sysCheckNamespace); err != nil {
					errs = append(errs, fmt.Sprintf("Failure during SysCheck pod %q status checking: %v", def.Name, err))
					break
				} else if pod.Status.Phase == v1.PodRunning {
					pods = append(pods, pod)
					break
				}
				time.Sleep(250 * time.Millisecond)
			}
			wg.Done()
		}()
	}
	wg.Wait()
	v.info("SysCheck pods on all Virtlet nodes are running")

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
	// TODO: this func should use timeouting context

	var wg sync.WaitGroup
	wg.Add(len(pods))

	errsNumber := 0
	for _, pod := range pods {
		go func() {
			errsNumber += check(pod)
			wg.Done()
		}()
	}

	wg.Wait()
	return errsNumber
}

func (v *validateCommand) chekcInAllSysChecks(pods []*v1.Pod, description, command string, check func(nodeName, out string) int) int {
	return doInAllPods(pods, func(pod *v1.Pod) int {
		errsNumber := 0
		var out bytes.Buffer
		_, err := v.client.ExecInContainer(
			pod.Name, "syscheck", pod.Namespace, nil, bufio.NewWriter(&out), nil,
			[]string{
				"/bin/sh", "-c",
				command,
			},
		)
		if err != nil {
			v.info("Error during verification of %s on node %q: %v", description, pod.Spec.NodeName, err)
			errsNumber += 1
		}

		return errsNumber + check(pod.Spec.NodeName, strings.TrimRight(out.String(), "\r\n"))
	})
}

func (v *validateCommand) checkCNI(pods []*v1.Pod) int {
	return v.chekcInAllSysChecks(
		pods, "CNI configuration",
		"find /mnt/etc/cni/net.d -name \"*.conf\" -o -name \"*.conflist\" -o -name \"*.json\" | wc -l",
		func(nodeName, out string) int {
			errsNumber := 0
			if i, err := strconv.Atoi(out); err != nil {
				v.info("Internal error during conunting CNI configuration files on %q: %v", nodeName, err)
				errsNumber += 1
			} else if i == 0 {
				v.info("Node %q does not have any CNI configuration in /etc/cni/net.d", nodeName)
				errsNumber += 1
			}
			return errsNumber
		},
	)
}

func (v *validateCommand) checkCRIProxy(pods []*v1.Pod) int {
	return v.chekcInAllSysChecks(
		pods, "CRI Proxy",
		"pgrep criproxy | while read pid ; do cat /proc/$pid/cmdline ; done",
		func(nodeName, out string) int {
			errsNumber := 0
			if len(out) == 0 {
				v.info("Node %q does not have CRI Proxy running", nodeName)
				errsNumber += 1
			} else if !strings.Contains(out, defaultCRIProxySockLocation) {
				v.info("CRI Proxy on node %q does not have %q as socket location", nodeName, defaultCRIProxySockLocation)
				errsNumber += 1
			}
			return errsNumber
		},
	)
}

func (v *validateCommand) checkKubeletArgs(pods []*v1.Pod) int {
	return v.chekcInAllSysChecks(
		pods, "kubelet configuration",
		"( pgrep kubelet ; pgrep hyperkube ) | while read pid ; do cat /proc/$pid/cmdline ; done",
		func(nodeName, out string) int {
			errsNumber := 0
			if len(out) == 0 {
				v.info("Internal error - kubelet not found on node %q", nodeName)
				errsNumber += 1
			} else {
				for _, arg := range []string{
					"--container-runtime=remote",
					"--container-runtime-endpoint=unix:///run/criproxy.sock",
					"--image-service-endpoint=unix:///run/criproxy.sock",
					"--enable-controller-attach-detach=false",
				} {
					if !strings.Contains(out, arg) {
						v.info("kubelet on node %q is missing %q option", nodeName, arg)
						errsNumber += 1
					}
				}
			}
			return errsNumber
		},
	)
}
