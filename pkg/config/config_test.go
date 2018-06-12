/*
Copyright 2018 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or ≈git-agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"bufio"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Mirantis/virtlet/tests/gm"
	"github.com/ghodss/yaml"
	"github.com/kballard/go-shellquote"
	flag "github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekube "k8s.io/client-go/kubernetes/fake"

	virtlet_v1 "github.com/Mirantis/virtlet/pkg/api/virtlet.k8s/v1"
	"github.com/Mirantis/virtlet/pkg/client/clientset/versioned/fake"
)

func TestDefaultVirtletConfig(t *testing.T) {
	gm.Verify(t, gm.NewYamlVerifier(GetDefaultConfig()))
}

func verifyEnv(t *testing.T, c *virtlet_v1.VirtletConfig) {
	envText := DumpEnv(c)
	gm.Verify(t, envText)
	fakeEnv := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(envText))
	lineRx := regexp.MustCompile("^export ([^=]*)=(.*)")
	for scanner.Scan() {
		s := scanner.Text()
		parts := lineRx.FindStringSubmatch(s)
		if parts == nil {
			t.Errorf("couldn't parse env string: %q", s)
		} else {
			subparts, err := shellquote.Split(parts[2])
			if err != nil {
				t.Errorf("couldn't parse env string: %q: %v", s, err)
			}
			if len(subparts) != 1 {
				t.Errorf("couldn't parse env string: %q", s)
			}
			fakeEnv[parts[1]] = subparts[0]
		}
	}
	binder := NewConfigBinder(nil)
	binder.lookupEnv = func(name string) (string, bool) {
		r, found := fakeEnv[name]
		return r, found
	}
	newConf := binder.GetConfig()
	// this is a special case (test-only flag)
	newConf.SkipImageTranslation = c.SkipImageTranslation
	if !reflect.DeepEqual(newConf, c) {
		origConfYaml, err := yaml.Marshal(c)
		if err != nil {
			t.Fatalf("Error marshalling yaml: %v", err)
		}
		newConfYaml, err := yaml.Marshal(newConf)
		if err != nil {
			t.Fatalf("Error marshalling yaml: %v", err)
		}
		t.Errorf("error reloading config from env. Was:\n%s\n--- became: ---\n%s", origConfYaml, newConfYaml)
	}
}

func TestMergeConfigs(t *testing.T) {
	pstr := func(s string) *string { return &s }
	pbool := func(b bool) *bool { return &b }
	pint := func(i int) *int { return &i }
	for _, tc := range []struct {
		name, args string
		configs    []*virtlet_v1.VirtletConfig
	}{
		{
			name: "defaults",
			args: "",
		},
		{
			name:    "defaults (explicitly set as a config)",
			args:    "",
			configs: []*virtlet_v1.VirtletConfig{GetDefaultConfig()},
		},
		{
			name: "all cli opts",
			args: "--fd-server-socket-path /some/fd/server.sock" +
				" --database-path /some/file.db" +
				" --image-download-protocol http" +
				" --image-dir /some/image/dir" +
				" --image-translation-configs-dir /some/translation/dir" +
				" --libvirt-uri qemu:///foobar" +
				" --raw-devices sd*" +
				" --listen /some/cri.sock" +
				" --disable-logging" +
				" --disable-kvm" +
				" --enable-sriov" +
				" --cni-bin-dir /some/cni/bin/dir" +
				" --cni-conf-dir /some/cni/conf/dir" +
				" --calico-subnet-size 22" +
				" --enable-regexp-image-translation=false",
		},
		{
			name: "all cli opts and explicit default config",
			args: "--fd-server-socket-path /some/fd/server.sock" +
				" --database-path /some/file.db" +
				" --image-download-protocol http" +
				" --image-dir /some/image/dir" +
				" --image-translation-configs-dir /some/translation/dir" +
				" --libvirt-uri qemu:///foobar" +
				" --raw-devices sd*" +
				" --listen /some/cri.sock" +
				" --disable-logging" +
				" --disable-kvm" +
				" --enable-sriov" +
				" --cni-bin-dir /some/cni/bin/dir" +
				" --cni-conf-dir /some/cni/conf/dir" +
				" --calico-subnet-size 22" +
				" --enable-regexp-image-translation=false",
			configs: []*virtlet_v1.VirtletConfig{GetDefaultConfig()},
		},
		{
			name: "opts and configs",
			args: "--raw-devices sd* --libvirt-uri qemu:///foobar",
			configs: []*virtlet_v1.VirtletConfig{
				{
					DisableKVM: pbool(true),
					RawDevices: pstr("vd*"),
				},
				{
					EnableSriov:      pbool(true),
					CalicoSubnetSize: pint(22),
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet("virtlet", flag.ContinueOnError)
			configBinder := NewConfigBinder(flags)
			if err := flags.Parse(strings.Split(tc.args, " ")); err != nil {
				t.Fatalf("error parsing flags: %v", err)
			}
			cfg := MergeConfigs(append(tc.configs, configBinder.GetConfig()))
			gm.Verify(t, gm.NewYamlVerifier(cfg))
			t.Run("env", func(t *testing.T) {
				verifyEnv(t, cfg)
			})
		})
	}
}

const (
	fullConfig = `
  config:
    fdServerSocketPath: /some/fd/server.sock
    databasePath: /some/file.db
    downloadProtocol: http
    imageTranslationConfigsDir: /some/translation/dir
    libvirtURI: qemu:///foobar
    rawDevices: sd*
    criSocketPath: /some/cri.sock
    disableLogging: true
    disableKVM: true
    enableSriov: true
    cniPluginDir: /some/cni/bin/dir
    cniConfigDir: /some/cni/conf/dir
    calicoSubnetSize: 22
    enableRegexpImageTranslation: false
    logLevel: 3`
	kubeNode1FullMapping = `
spec:
  nodeName: kube-node-1
  priority: 10` + fullConfig
	labelAFullMapping = `
spec:
  nodeSelector:
    label-a: "1"` + fullConfig
	anotherMapping1 = `
spec:
  nodeName: kube-node-1
  priority: 10
  config:
    enableSriov: false
    disableKVM: true`
	anotherMapping2 = `
spec:
  nodeSelector:
    label-b: "1"
  priority: 1
  config:
    rawDevices: vd*
    downloadProtocol: http`
	anotherMapping3 = `
spec:
  nodeName: kube-node-2
  priority: 10
  config:
    disableLogging: true`
	anotherMapping4 = `
spec:
  nodeSelector:
    label-a: "1"
  config:
    enableSriov: true
    rawDevices: sd*`
	sampleLocalConfig = `
disableKVM: false
`
)

func TestConfigForNode(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mappings    []string
		nodeName    string
		nodeLabels  map[string]string
		localConfig string
	}{
		{
			name: "no mappings",
		},
		{
			name:       "mapping by node name",
			nodeName:   "kube-node-1",
			nodeLabels: map[string]string{"label-a": "1"},
			mappings:   []string{kubeNode1FullMapping},
		},
		{
			name:       "mapping by node labels",
			nodeName:   "kube-node-1",
			nodeLabels: map[string]string{"label-a": "1"},
			mappings:   []string{labelAFullMapping},
		},
		{
			name:       "mapping by node name (no match)",
			nodeName:   "kube-node-2",
			nodeLabels: map[string]string{"label-a": "1"},
			mappings:   []string{kubeNode1FullMapping},
		},
		{
			name:       "mapping by node labels (no match)",
			nodeName:   "kube-node-1",
			nodeLabels: map[string]string{"label-x": "1"},
			mappings:   []string{labelAFullMapping},
		},
		{
			name:       "mapping by node name and multiple labels",
			nodeName:   "kube-node-1",
			nodeLabels: map[string]string{"label-a": "1", "label-b": "1"},
			mappings:   []string{anotherMapping1, anotherMapping2, anotherMapping3, anotherMapping4},
		},
		{
			name:        "mapping by node name and multiple labels and a local config",
			nodeName:    "kube-node-1",
			nodeLabels:  map[string]string{"label-a": "1", "label-b": "1"},
			mappings:    []string{anotherMapping1, anotherMapping2, anotherMapping3, anotherMapping4},
			localConfig: sampleLocalConfig,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mappings []virtlet_v1.VirtletConfigMapping
			for _, s := range tc.mappings {
				var m virtlet_v1.VirtletConfigMapping
				if err := yaml.Unmarshal([]byte(s), &m); err != nil {
					t.Fatalf("Error parsing yaml: %v", err)
				}
				mappings = append(mappings, m)
				copiedMapping := m.DeepCopyObject()
				if !reflect.DeepEqual(copiedMapping, &m) {
					t.Fatal("deep copy failed")
				}
			}
			var localConfig *virtlet_v1.VirtletConfig
			if tc.localConfig != "" {
				if err := yaml.Unmarshal([]byte(tc.localConfig), &localConfig); err != nil {
					t.Fatalf("Error parsing yaml: %v", err)
				}
			}
			cfg := configForNode(mappings, localConfig, tc.nodeName, tc.nodeLabels)
			gm.Verify(t, gm.NewYamlVerifier(cfg))
		})
	}
}

func TestLoadMappings(t *testing.T) {
	pstr := func(s string) *string { return &s }
	pbool := func(b bool) *bool { return &b }
	nc := NewNodeConfig(nil)
	nc.kubeClient = fakekube.NewSimpleClientset(
		&v1.Node{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: "kube-node-1",
				Labels: map[string]string{
					"label-a": "1",
					"label-b": "1",
				},
			},
		})
	nc.virtletClient = fake.NewSimpleClientset(
		&virtlet_v1.VirtletConfigMapping{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "mapping-1",
				Namespace: "kube-system",
			},
			Spec: virtlet_v1.VirtletConfigMappingSpec{
				NodeName: "kube-node-1",
				Config: &virtlet_v1.VirtletConfig{
					EnableSriov: pbool(true),
					DisableKVM:  pbool(true),
				},
			},
		},
		&virtlet_v1.VirtletConfigMapping{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "mapping-2",
				Namespace: "kube-system",
			},
			Spec: virtlet_v1.VirtletConfigMappingSpec{
				NodeSelector: map[string]string{"label-a": "1"},
				Config: &virtlet_v1.VirtletConfig{
					RawDevices: pstr("sd*"),
				},
			},
		})
	cfg, err := nc.LoadConfig(&virtlet_v1.VirtletConfig{
		EnableSriov: pbool(false),
	}, "kube-node-1")
	if err != nil {
		t.Fatalf("LoadConfig(): %v", err)
	}
	gm.Verify(t, gm.NewYamlVerifier(cfg))
}

func TestGenerateDoc(t *testing.T) {
	gm.Verify(t, GenerateDoc())
}
