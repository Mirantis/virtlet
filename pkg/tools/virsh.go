/*
Copyright 2018 Mirantis

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
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/renstrom/dedent"
	"github.com/spf13/cobra"
)

// virshCommand contains the data needed by the virsh subcommand
// which allows one to execute virsh commands for a VM pod.
type virshCommand struct {
	client   KubeClient
	nodeName string
	args     []string
	out      io.Writer
}

func NewVirshCmd(client KubeClient, out io.Writer) *cobra.Command {
	virsh := &virshCommand{client: client, out: out}
	cmd := &cobra.Command{
		Use:   "virsh",
		Short: "execute a virsh command",
		Long: dedent.Dedent(`
                        This command executes libvirt virsh command.

                        A VM pod name in the form @podname is translated to the
                        corresponding libvirt domain name. If @podname is specified,
                        the target k8s node name is inferred automatically based
                        on the information of the VM pod. In case if no @podname
                        is specified, it's necessary to provide the node name
                        using the --node flag.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			virsh.args = args
			return virsh.Run()
		},
	}
	cmd.Flags().StringVar(&virsh.nodeName, "node", "", "the name of the target node")
	return cmd
}

// Run executes the command.
func (v *virshCommand) Run() error {
	if len(v.args) == 0 {
		return errors.New("missing virsh argument(s)")
	}
	var nodeName, virtletPodName string
	var realArgs []string
	for _, arg := range v.args {
		if len(arg) < 2 || arg[0] != '@' {
			realArgs = append(realArgs, arg)
			continue
		}
		podName := arg[1:]
		vmPodInfo, err := v.client.GetVMPodInfo(podName)
		switch {
		case err != nil:
			return fmt.Errorf("can't get VM pod info for %q: %v", podName, err)
		case nodeName == "":
			nodeName = vmPodInfo.NodeName
			virtletPodName = vmPodInfo.VirtletPodName
		case nodeName != vmPodInfo.NodeName:
			return errors.New("can't reference VM pods that run on different nodes")
		}
		realArgs = append(realArgs, vmPodInfo.LibvirtDomainName())
	}
	switch {
	case v.nodeName == "" && nodeName == "":
		return errors.New("please specify Virtlet node with --node")
	case nodeName == "":
		var err error
		virtletPodName, err = v.client.GetVirtletPodNameForNode(v.nodeName)
		if err != nil {
			return fmt.Errorf("couldn't get Virtlet pod name for node %q: %v", v.nodeName, err)
		}
	case v.nodeName != "" && nodeName != v.nodeName:
		return errors.New("--node specifies a node other than one that runs the VM pod")
	}

	exitCode, err := v.client.ExecInContainer(virtletPodName, "libvirt", "kube-system", nil, v.out, os.Stderr, append([]string{"virsh"}, realArgs...))
	if err != nil {
		return fmt.Errorf("error executing virsh in Virtlet pod %q: %v", virtletPodName, err)
	}
	if exitCode != 0 {
		return fmt.Errorf("virsh returned non-zero exit code %d", exitCode)
	}

	return nil
}
