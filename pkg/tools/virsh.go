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
	client         KubeClient
	nodeName       string
	args           []string
	out            io.Writer
	realArgs       []string
	domainNodeName string
	virtletPodName string
}

// NewVirshCmd returns a cobra.Command that executes virsh for a VM pod.
func NewVirshCmd(client KubeClient, out io.Writer) *cobra.Command {
	virsh := &virshCommand{client: client, out: out}
	cmd := &cobra.Command{
		Use:   "virsh [flags] virsh_command -- [virsh_command_args...]",
		Short: "Execute a virsh command",
		Long: dedent.Dedent(`
                        This command executes libvirt virsh command.

                        A VM pod name in the form @podname is translated to the
                        corresponding libvirt domain name. If @podname is specified,
                        the target k8s node name is inferred automatically based
                        on the information of the VM pod. In case if no @podname
                        is specified, the command is executed on every node
                        and the output for every node is prepended with a line
                        with the node name and corresponding Virtlet pod name.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			virsh.args = args
			return virsh.Run()
		},
	}
	cmd.Flags().StringVar(&virsh.nodeName, "node", "", "the name of the target node")
	return cmd
}

func (v *virshCommand) processArgs() error {
	if len(v.args) == 0 {
		return errors.New("missing virsh argument(s)")
	}

	v.realArgs = nil
	for _, arg := range v.args {
		if len(arg) < 2 || arg[0] != '@' {
			v.realArgs = append(v.realArgs, arg)
			continue
		}
		podName := arg[1:]
		vmPodInfo, err := v.client.GetVMPodInfo(podName)
		switch {
		case err != nil:
			return fmt.Errorf("can't get VM pod info for %q: %v", podName, err)
		case v.domainNodeName == "":
			v.domainNodeName = vmPodInfo.NodeName
			v.virtletPodName = vmPodInfo.VirtletPodName
		case v.domainNodeName != vmPodInfo.NodeName:
			return errors.New("can't reference VM pods that run on different nodes")
		}
		v.realArgs = append(v.realArgs, vmPodInfo.LibvirtDomainName())
	}

	return nil
}

func (v *virshCommand) runInVirtletPod(virtletPodName string) error {
	exitCode, err := v.client.ExecInContainer(virtletPodName, "libvirt", "kube-system", nil, v.out, os.Stderr, append([]string{"virsh"}, v.realArgs...))
	if err != nil {
		return fmt.Errorf("error executing virsh in Virtlet pod %q: %v", virtletPodName, err)
	}
	if exitCode != 0 {
		return fmt.Errorf("virsh returned non-zero exit code %d", exitCode)
	}
	return nil
}

func (v *virshCommand) runOnAllNodes() error {
	podNames, nodeNames, err := v.client.GetVirtletPodAndNodeNames()
	if err != nil {
		return err
	}
	gotErrors := false
	for n, nodeName := range nodeNames {
		fmt.Fprintf(v.out, "*** node: %s pod: %s ***\n", nodeName, podNames[n])
		if err := v.runInVirtletPod(podNames[n]); err != nil {
			fmt.Fprintf(v.out, "ERROR: %v\n", err)
			gotErrors = true
		}
		fmt.Fprint(v.out, "\n")
	}
	if gotErrors {
		return errors.New("some of the nodes returned errors")
	}
	return nil
}

// Run executes the command.
func (v *virshCommand) Run() error {
	if err := v.processArgs(); err != nil {
		return err
	}
	switch {
	case v.nodeName == "" && v.domainNodeName == "":
		return v.runOnAllNodes()
	case v.domainNodeName == "":
		var err error
		v.virtletPodName, err = v.client.GetVirtletPodNameForNode(v.nodeName)
		if err != nil {
			return fmt.Errorf("couldn't get Virtlet pod name for node %q: %v", v.nodeName, err)
		}
	case v.nodeName != "" && v.domainNodeName != v.nodeName:
		return errors.New("--node specifies a node other than one that runs the VM pod")
	}

	return v.runInVirtletPod(v.virtletPodName)
}
