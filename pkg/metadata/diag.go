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

package metadata

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/ghodss/yaml"

	"github.com/Mirantis/virtlet/pkg/diag"
	"github.com/Mirantis/virtlet/pkg/metadata/types"
)

const (
	indentStr = "  "
)

type metadataDumper struct {
	store  Store
	out    io.Writer
	indent int
}

func newMetadataDumper(store Store, out io.Writer) *metadataDumper {
	return &metadataDumper{store: store, out: out}
}

func (d *metadataDumper) withAddedIndent(toCall func()) {
	d.indent++
	defer func() {
		d.indent--
	}()
	toCall()
}

func (d *metadataDumper) indentString() string {
	return strings.Repeat(indentStr, d.indent)
}

func (d *metadataDumper) outputYaml(o interface{}) {
	out, err := yaml.Marshal(o)
	if err != nil {
		fmt.Fprintf(d.out, "<error marshalling the object: %v", err)
	}
	indentStr := d.indentString()
	for _, l := range strings.Split(string(out), "\n") {
		if l != "" {
			l = indentStr + l
		}
		fmt.Fprintln(d.out, l)
	}
}

func (d *metadataDumper) output(format string, args ...interface{}) {
	fmt.Fprintf(d.out, d.indentString()+format+"\n", args...)
}

func (d *metadataDumper) outputError(description string, err error) {
	d.output("<error: %s: %v>", description, err)
}

func (d *metadataDumper) dump() {
	d.output("Sandboxes:")
	sandboxes, err := d.store.ListPodSandboxes(nil)
	switch {
	case err != nil:
		d.outputError("can't list sandboxes", err)
	case len(sandboxes) == 0:
		d.output("no sandboxes found")
	default:
		d.withAddedIndent(func() {
			for _, smeta := range sandboxes {
				if sinfo, err := smeta.Retrieve(); err != nil {
					d.outputError("can't retrieve sandbox", err)
				} else if err := d.dumpSandbox(smeta.GetID(), sinfo); err != nil {
					d.outputError("dumping sandbox", err)
				}
			}
		})
	}

	d.output("Images:")
	images, err := d.store.ImagesInUse()
	switch {
	case err != nil:
		d.outputError("can't list images", err)
	case len(images) == 0:
		d.output("no images found")
	default:
		d.withAddedIndent(func() {
			for image := range images {
				d.output(image)
			}
		})
	}
}

func (d *metadataDumper) dumpSandbox(podID string, sandbox *types.PodSandboxInfo) error {
	d.output("Sandbox ID: %v", podID)
	d.withAddedIndent(func() {
		d.outputYaml(sandbox)

		d.output("Containers:")
		containers, err := d.store.ListPodContainers(podID)
		switch {
		case err != nil:
			d.outputError("can't retrieve the list of containers", err)
		case len(containers) == 0:
			d.output("no containers found")
		default:
			d.withAddedIndent(func() {
				for _, cmeta := range containers {
					d.output("Container ID: %s", cmeta.GetID())
					if cinfo, err := cmeta.Retrieve(); err != nil {
						d.outputError("can't retrieve container metadata", err)
					} else {
						d.withAddedIndent(func() { d.outputYaml(cinfo) })
					}
				}
			})
		}
	})

	return nil
}

// GetMetadataDumpSource returns a Source that dumps Virtlet metadata.
func GetMetadataDumpSource(store Store) diag.Source {
	return diag.NewSimpleTextSource("txt", func() (string, error) {
		var out bytes.Buffer
		dumper := newMetadataDumper(store, &out)
		dumper.dump()
		return out.String(), nil
	})
}
