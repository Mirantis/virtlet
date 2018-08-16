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

package config

import (
	"fmt"
	"math"
	"sort"

	virtletclient "github.com/Mirantis/virtlet/pkg/client/clientset/versioned"
	flag "github.com/spf13/pflag"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	virtlet_v1 "github.com/Mirantis/virtlet/pkg/api/virtlet.k8s/v1"
)

const (
	defaultFDServerSocketPath = "/var/lib/virtlet/tapfdserver.sock"
	fdServerSocketPathEnv     = "VIRTLET_FD_SERVER_SOCKET_PATH"

	defaultDatabasePath = "/var/lib/virtlet/virtlet.db"
	databasePathEnv     = "VIRTLET_DATABASE_PATH"

	defaultDownloadProtocol  = "https"
	imageDownloadProtocolEnv = "VIRTLET_DOWNLOAD_PROTOCOL"

	defaultImageDir = "/var/lib/libvirt/images"
	imageDirEnv     = "VIRTLET_IMAGE_DIR"

	defaultImageTranslationConfigsDir = "/etc/virtlet/images"
	imageTranslationsConfigDirEnv     = "VIRTLET_IMAGE_TRANSLATIONS_DIR"

	defaultLibvirtURI = "qemu:///system"
	libvirtURIEnv     = "VIRTLET_LIBVIRT_URI"

	defaultRawDevices = "loop*"
	rawDevicesEnv     = "VIRTLET_RAW_DEVICES"

	defaultCRISocketPath = "/run/virtlet.sock"
	criSocketPathEnv     = "VIRTLET_CRI_SOCKET_PATH"

	disableLoggingEnv = "VIRTLET_DISABLE_LOGGING"
	disableKVMEnv     = "VIRTLET_DISABLE_KVM"
	enableSriovEnv    = "VIRTLET_SRIOV_SUPPORT"

	defaultCNIPluginDir = "/opt/cni/bin"
	cniPluginDirEnv     = "VIRTLET_CNI_PLUGIN_DIR"

	defaultCNIConfigDir = "/etc/cni/net.d"
	cniConfigDirEnv     = "VIRTLET_CNI_CONFIG_DIR"

	defaultCalicoSubnet = 24
	calicoSubnetEnv     = "VIRTLET_CALICO_SUBNET"

	enableRegexpImageTranslationEnv = "IMAGE_REGEXP_TRANSLATION"
	logLevelEnv                     = "VIRTLET_LOGLEVEL"

	defaultCPUModel = ""
	cpuModelEnv     = "VIRTLET_CPU_MODEL"
)

func configFieldSet(c *virtlet_v1.VirtletConfig) *fieldSet {
	var fs fieldSet
	fs.addStringField("fdServerSocketPath", "fd-server-socket-path", "", "Path to fd server socket", fdServerSocketPathEnv, defaultFDServerSocketPath, &c.FDServerSocketPath)
	fs.addStringField("databasePath", "database-path", "", "Path to the virtlet database", databasePathEnv, defaultDatabasePath, &c.DatabasePath)
	fs.addStringFieldWithPattern("downloadProtocol", "image-download-protocol", "", "Image download protocol. Can be https or http", imageDownloadProtocolEnv, defaultDownloadProtocol, "^https?$", &c.DownloadProtocol)
	fs.addStringField("imageDir", "image-dir", "", "Image directory", imageDirEnv, defaultImageDir, &c.ImageDir)
	fs.addStringField("imageTranslationConfigsDir", "image-translation-configs-dir", "", "Image name translation configs directory", imageTranslationsConfigDirEnv, defaultImageTranslationConfigsDir, &c.ImageTranslationConfigsDir)
	// SkipImageTranslation doesn't have corresponding flag or env var as it's only used by tests
	fs.addBoolField("skipImageTranslation", "", "", "", "", false, &c.SkipImageTranslation)
	fs.addStringField("libvirtURI", "libvirt-uri", "", "Libvirt connection URI", libvirtURIEnv, defaultLibvirtURI, &c.LibvirtURI)
	fs.addStringField("rawDevices", "raw-devices", "", "Comma separated list of raw device glob patterns which VMs can access (without '/dev/' prefix)", rawDevicesEnv, defaultRawDevices, &c.RawDevices)
	fs.addStringField("criSocketPath", "listen", "", "The path to UNIX domain socket for CRI service to listen on", criSocketPathEnv, defaultCRISocketPath, &c.CRISocketPath)
	fs.addBoolField("disableLogging", "disable-logging", "", "Display logging and the streamer", disableLoggingEnv, false, &c.DisableLogging)
	fs.addBoolField("disableKVM", "disable-kvm", "", "Forcibly disable KVM support", disableKVMEnv, false, &c.DisableKVM)
	fs.addBoolField("enableSriov", "enable-sriov", "", "Enable SR-IOV support", enableSriovEnv, false, &c.EnableSriov)
	fs.addStringField("cniPluginDir", "cni-bin-dir", "", "Path to CNI plugin binaries", cniPluginDirEnv, defaultCNIPluginDir, &c.CNIPluginDir)
	fs.addStringField("cniConfigDir", "cni-conf-dir", "", "Path to the CNI configuration directory", cniConfigDirEnv, defaultCNIConfigDir, &c.CNIConfigDir)
	fs.addIntField("calicoSubnetSize", "calico-subnet-size", "", "Calico subnet size to use", calicoSubnetEnv, defaultCalicoSubnet, 0, 32, &c.CalicoSubnetSize)
	fs.addBoolField("enableRegexpImageTranslation", "enable-regexp-image-translation", "", "Enable regexp image name translation", enableRegexpImageTranslationEnv, true, &c.EnableRegexpImageTranslation)
	fs.addStringField("cpuModel", "cpu-model", "", "CPU model to use in libvirt domain definition (libvirt's default value will be used if not set)", cpuModelEnv, defaultCPUModel, &c.CPUModel)
	// this field duplicates glog's --v, so no option for it, which is signified
	// by "+" here (it's only for doc)
	fs.addIntField("logLevel", "+v", "", "Log level to use", logLevelEnv, 1, 0, math.MaxInt32, &c.LogLevel)
	return &fs
}

// GetDefaultConfig returns a VirtletConfig with all values set to default
func GetDefaultConfig() *virtlet_v1.VirtletConfig {
	var c virtlet_v1.VirtletConfig
	configFieldSet(&c).applyDefaults()
	return &c
}

// Override replaces the values in the target config with those
// which are set in the other config.
func Override(target, other *virtlet_v1.VirtletConfig) {
	configFieldSet(target).override(configFieldSet(other))
}

// DumpEnv returns a string with environment variable settings
// corresponding to the VirtletConfig.
func DumpEnv(c *virtlet_v1.VirtletConfig) string {
	return configFieldSet(c).dumpEnv()
}

// GenerateDoc generates a markdown document with a table describing
// all the configuration settings.
func GenerateDoc() string {
	return configFieldSet(&virtlet_v1.VirtletConfig{}).generateDoc()
}

func mappingMatches(cm virtlet_v1.VirtletConfigMapping, nodeName string, nodeLabels map[string]string) bool {
	if cm.Spec.Config == nil {
		return false
	}
	if cm.Spec.NodeName != "" && cm.Spec.NodeName != nodeName {
		return false
	}
	for label, value := range cm.Spec.NodeSelector {
		actual, found := nodeLabels[label]
		if !found || actual != value {
			return false
		}
	}
	return true
}

// MergeConfigs merges several Virtlet configs together, with
// configs going later taking precedence.
func MergeConfigs(configs []*virtlet_v1.VirtletConfig) *virtlet_v1.VirtletConfig {
	var cfg *virtlet_v1.VirtletConfig
	for _, cur := range configs {
		if cfg == nil {
			cfg = cur
		} else {
			Override(cfg, cur)
		}
	}
	return cfg
}

// Binder is used to extract Virtlet config from a FlagSet.
type Binder struct {
	flagSet   *flag.FlagSet
	config    *virtlet_v1.VirtletConfig
	fieldSet  *fieldSet
	lookupEnv envLookup
}

// NewBinder returns a new Binder.
func NewBinder(flagSet *flag.FlagSet) *Binder {
	config := &virtlet_v1.VirtletConfig{}
	fs := configFieldSet(config)
	fs.applyDefaults()
	if flagSet != nil {
		fs.addFlags(flagSet)
	}
	return &Binder{
		flagSet:  flagSet,
		config:   config,
		fieldSet: fs,
	}
}

// GetConfig returns the config that only includes the fields that
// were explicitly set in the flags. It should be called after parsing
// the flags.
func (b *Binder) GetConfig() *virtlet_v1.VirtletConfig {
	b.fieldSet.clearFieldsNotInFlagSet(b.flagSet)
	b.fieldSet.setFromEnv(b.lookupEnv)
	return b.config
}

// configForNode gets virtlet_v1.VirtletConfig for the specified node name and labels.
func configForNode(mappings []virtlet_v1.VirtletConfigMapping, localConfig *virtlet_v1.VirtletConfig, nodeName string, nodeLabels map[string]string) *virtlet_v1.VirtletConfig {
	cfg := GetDefaultConfig()
	var sortedMappings []virtlet_v1.VirtletConfigMapping
	for _, m := range mappings {
		if mappingMatches(m, nodeName, nodeLabels) {
			sortedMappings = append(sortedMappings, m)
		}
	}
	sort.Slice(sortedMappings, func(i, j int) bool {
		a, b := sortedMappings[i], sortedMappings[j]
		// Iitems that go later in the list take precedence.
		return a.Spec.Priority < b.Spec.Priority
	})

	configs := []*virtlet_v1.VirtletConfig{cfg}
	for _, m := range sortedMappings {
		configs = append(configs, m.Spec.Config)
	}
	if localConfig != nil {
		configs = append(configs, localConfig)
	}
	return MergeConfigs(configs)
}

// NodeConfig is used to retrieve Virtlet configuration for the current
// node.
type NodeConfig struct {
	clientCfg     clientcmd.ClientConfig
	kubeClient    kubernetes.Interface
	virtletClient virtletclient.Interface
}

// NewNodeConfig creates a new NodeConfig
func NewNodeConfig(clientCfg clientcmd.ClientConfig) *NodeConfig {
	return &NodeConfig{clientCfg: clientCfg}
}

func (nc *NodeConfig) setup() error {
	if nc.kubeClient != nil {
		return nil
	}

	config, err := nc.clientCfg.ClientConfig()
	if err != nil {
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("can't create kubernetes api client: %v", err)
	}

	virtletClient, err := virtletclient.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("can't create Virtlet api client: %v", err)
	}

	nc.kubeClient = kubeClient
	nc.virtletClient = virtletClient
	return nil
}

// LoadConfig loads the configuration for the specified node.
func (nc *NodeConfig) LoadConfig(localConfig *virtlet_v1.VirtletConfig, nodeName string) (*virtlet_v1.VirtletConfig, error) {
	if err := nc.setup(); err != nil {
		return nil, err
	}

	node, err := nc.kubeClient.CoreV1().Nodes().Get(nodeName, meta_v1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("can't get node info for node %q: %v", nodeName, err)
	}

	mappingList, err := nc.virtletClient.VirtletV1().VirtletConfigMappings("kube-system").List(meta_v1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list Virtlet config mappings: %v", err)
	}

	return configForNode(mappingList.Items, localConfig, nodeName, node.Labels), nil
}
