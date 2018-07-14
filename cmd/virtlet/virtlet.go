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

package main

import (
	goflag "flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/golang/glog"
	flag "github.com/spf13/pflag"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/Mirantis/virtlet/pkg/api/virtlet.k8s/v1"
	"github.com/Mirantis/virtlet/pkg/cni"
	"github.com/Mirantis/virtlet/pkg/config"
	"github.com/Mirantis/virtlet/pkg/diag"
	"github.com/Mirantis/virtlet/pkg/libvirttools"
	"github.com/Mirantis/virtlet/pkg/manager"
	"github.com/Mirantis/virtlet/pkg/nsfix"
	"github.com/Mirantis/virtlet/pkg/tapmanager"
	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/pkg/version"
)

const (
	wantTapManagerEnv  = "WANT_TAP_MANAGER"
	nodeNameEnv        = "KUBE_NODE_NAME"
	diagSocket         = "/run/virtlet-diag.sock"
	netnsDiagCommand   = `if [ -d /var/run/netns ]; then cd /var/run/netns; for ns in *; do echo "*** ${ns} ***"; ip netns exec "${ns}" ip a; ip netns exec "${ns}" ip r; echo; done; fi`
	criproxyLogCommand = `nsenter -t 1 -m -u -i journalctl -xe -u criproxy -n 20000 --no-pager || true`
	qemuLogDir         = "/var/log/libvirt/qemu"
)

var (
	dumpConfig     = flag.Bool("dump-config", false, "Dump node-specific Virtlet config as a shell script and exit")
	dumpDiag       = flag.Bool("diag", false, "Dump diagnostics as JSON and exit")
	displayVersion = flag.Bool("version", false, "Display version and exit")
	versionFormat  = flag.String("version-format", "text", "Version format to use (text, short, json, yaml)")
)

func configWithDefaults(cfg *v1.VirtletConfig) *v1.VirtletConfig {
	r := config.GetDefaultConfig()
	config.Override(r, cfg)
	return r
}

func runVirtlet(config *v1.VirtletConfig, clientCfg clientcmd.ClientConfig, diagSet *diag.Set) {
	manager := manager.NewVirtletManager(config, nil, clientCfg, diagSet)
	if err := manager.Run(); err != nil {
		glog.Errorf("Error: %v", err)
		os.Exit(1)
	}
}

func runTapManager(config *v1.VirtletConfig) {
	cniClient, err := cni.NewClient(*config.CNIPluginDir, *config.CNIConfigDir)
	if err != nil {
		glog.Errorf("Error initializing CNI client: %v", err)
		os.Exit(1)
	}
	src, err := tapmanager.NewTapFDSource(cniClient, *config.EnableSriov, *config.CalicoSubnetSize)
	if err != nil {
		glog.Errorf("Error creating tap fd source: %v", err)
		os.Exit(1)
	}
	os.Remove(*config.FDServerSocketPath) // FIXME
	s := tapmanager.NewFDServer(*config.FDServerSocketPath, src)
	if err = s.Serve(); err != nil {
		glog.Errorf("FD server returned error: %v", err)
		os.Exit(1)
	}
	if err := libvirttools.ChownForEmulator(*config.FDServerSocketPath, false); err != nil {
		glog.Warningf("Couldn't set tapmanager socket permissions: %v", err)
	}
	for {
		time.Sleep(1000 * time.Hour)
	}
}

func printVersion() {
	out, err := version.Get().ToBytes(*versionFormat)
	if err == nil {
		_, err = os.Stdout.Write(out)
	}
	if err != nil {
		glog.Errorf("Error printing version info: %v", err)
		os.Exit(1)
	}
}

func setLogLevel(config *v1.VirtletConfig) {
	goflag.CommandLine.Parse([]string{
		fmt.Sprintf("-v=%d", config.LogLevel),
		"-logtostderr=true",
	})
}

func runDiagServer() *diag.Set {
	diagSet := diag.NewDiagSet()
	diagSet.RegisterDiagSource("ip-a", diag.NewCommandSource("txt", []string{"ip", "a"}))
	diagSet.RegisterDiagSource("ip-r", diag.NewCommandSource("txt", []string{"ip", "r"}))
	diagSet.RegisterDiagSource("psaux", diag.NewCommandSource("txt", []string{"ps", "aux"}))
	diagSet.RegisterDiagSource("netns", diag.NewCommandSource("txt", []string{"/bin/bash", "-c", netnsDiagCommand}))
	diagSet.RegisterDiagSource("criproxy", diag.NewCommandSource("log", []string{"/bin/bash", "-c", criproxyLogCommand}))
	diagSet.RegisterDiagSource("libvirt-logs", diag.NewLogDirSource(qemuLogDir))
	diagSet.RegisterDiagSource("stack", diag.StackDumpSource)
	server := diag.NewServer(diagSet)
	go func() {
		err := server.Serve(diagSocket, nil)
		glog.V(1).Infof("Diag server returned: %v", err)
	}()
	return diagSet
}

func doDiag() {
	dr, err := diag.RetrieveDiagnostics(diagSocket)
	if err != nil {
		glog.Errorf("Failed to retrieve diagnostics: %v", err)
		os.Exit(1)
	}
	os.Stdout.Write(dr.ToJSON())
}

func main() {
	nsfix.HandleReexec()
	clientCfg := utils.BindFlags(flag.CommandLine)
	var cb *config.Binder
	cb = config.NewBinder(flag.CommandLine)
	flag.Parse()
	localConfig := cb.GetConfig()

	rand.Seed(time.Now().UnixNano())
	setLogLevel(configWithDefaults(localConfig))
	switch {
	case *displayVersion:
		printVersion()
	case *dumpConfig:
		nodeConfig := config.NewNodeConfig(clientCfg)
		nodeName := os.Getenv(nodeNameEnv)
		cfg, err := nodeConfig.LoadConfig(localConfig, nodeName)
		if err != nil {
			glog.Warningf("Failed to load per-node configs, using local config only: %v", err)
			cfg = localConfig
		}
		if _, err := os.Stdout.Write([]byte(config.DumpEnv(cfg))); err != nil {
			glog.Errorf("Error writing config: %v", err)
			os.Exit(1)
		}
	case *dumpDiag:
		doDiag()
	default:
		localConfig = configWithDefaults(localConfig)
		go runTapManager(localConfig)
		diagSet := runDiagServer()
		runVirtlet(localConfig, clientCfg, diagSet)
	}
}
