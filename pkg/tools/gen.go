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
	"io"

	"github.com/spf13/cobra"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/Mirantis/virtlet/pkg/config"
	"github.com/Mirantis/virtlet/pkg/version"
)

const (
	sourceYamlFile = "deploy/data/virtlet-ds.yaml"
	virtletImage   = "mirantis/virtlet"
)

// genCommand is used to generate Kubernetes YAML for Virtlet deployment
type genCommand struct {
	out    io.Writer
	dev    bool
	compat bool
	crd    bool
	tag    string
}

// NewGenCmd returns a cobra.Command that generates Kubernetes YAML for Virtlet
// deployment.
func NewGenCmd(out io.Writer) *cobra.Command {
	g := &genCommand{out: out}
	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate Kubernetes YAML for Virtlet deployment",
		Long:  "This command produces YAML suitable for use with kubectl apply -f -",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("This command does not accept arguments")
			}
			return g.Run()
		},
	}
	cmd.Flags().BoolVar(&g.dev, "dev", false, "Development mode for use with kubeadm-dind-cluster")
	cmd.Flags().BoolVar(&g.compat, "compat", false, "Produce YAML that's compatible with older Kubernetes versions")
	cmd.Flags().BoolVar(&g.crd, "crd", false, "Dump CRD definitions only")
	cmd.Flags().StringVar(&g.tag, "tag", version.Get().ImageTag, "Set virtlet image tag")
	return cmd
}

func (g *genCommand) getYaml() ([]byte, error) {
	var objs []runtime.Object
	if !g.crd {
		bs, err := Asset(sourceYamlFile)
		if err != nil {
			return bs, err
		}

		if objs, err = LoadYaml(bs); err != nil {
			return nil, err
		}
		if len(objs) == 0 {
			return nil, errors.New("source yaml is empty")
		}

		ds, ok := objs[0].(*apps.DaemonSet)
		if !ok {
			return nil, errors.New("the first object is not a DaemonSet")
		}
		if g.dev {
			applyDev(ds)
		}
		if g.compat {
			applyCompat(ds)
		}
		if g.tag != "" {
			applyTag(ds, g.tag)
		}
	}

	objs = append(objs, config.GetCRDDefinitions()...)
	return ToYaml(objs)
}

// Run executes the command.
func (g *genCommand) Run() error {
	bs, err := g.getYaml()
	if err != nil {
		return err
	}
	if _, err := g.out.Write(bs); err != nil {
		return err
	}
	return nil
}

func walkContainers(ds *apps.DaemonSet, toCall func(c *v1.Container)) {
	initContainers := ds.Spec.Template.Spec.InitContainers
	for n := range initContainers {
		toCall(&initContainers[n])
	}
	containers := ds.Spec.Template.Spec.Containers
	for n := range containers {
		toCall(&containers[n])
	}
}

func walkMounts(ds *apps.DaemonSet, toCall func(m *v1.VolumeMount)) {
	walkContainers(ds, func(c *v1.Container) {
		for i := range c.VolumeMounts {
			toCall(&c.VolumeMounts[i])
		}
	})
}

func applyDev(ds *apps.DaemonSet) {
	ds.Spec.Template.Spec.Volumes = append(ds.Spec.Template.Spec.Volumes, v1.Volume{
		Name: "dind",
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: "/dind",
			},
		},
	})
	volMount := v1.VolumeMount{
		Name:      "dind",
		MountPath: "/dind",
	}
	walkContainers(ds, func(c *v1.Container) {
		c.VolumeMounts = append(c.VolumeMounts, volMount)
	})
}

func applyCompat(ds *apps.DaemonSet) {
	walkMounts(ds, func(v *v1.VolumeMount) {
		if v.Name == "run" || v.Name == "k8s-pods-dir" {
			v.MountPath += ":shared"
			v.MountPropagation = nil
		}
	})
}

func applyTag(ds *apps.DaemonSet, tag string) {
	walkContainers(ds, func(c *v1.Container) {
		if c.Image == virtletImage {
			c.Image += ":" + tag
		}
	})
}
