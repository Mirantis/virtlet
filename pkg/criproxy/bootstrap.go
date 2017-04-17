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

package criproxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/golang/glog"

	// TODO: switch to https://github.com/docker/docker/tree/master/client
	// Docker version used in k8s is too old for it
	dockerclient "github.com/docker/engine-api/client"
	dockertypes "github.com/docker/engine-api/types"
	dockercontainer "github.com/docker/engine-api/types/container"
	dockerfilters "github.com/docker/engine-api/types/filters"

	// FIXME: use client-go
	// In particular, see https://github.com/kubernetes/client-go/blob/master/examples/in-cluster/main.go
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"

	cfg "k8s.io/kubernetes/pkg/apis/componentconfig/v1alpha1"
)

const (
	BusyboxImageName = "busybox:1.26.2"
	// TODO: use the same constant/setting in different parts of code
	proxyRuntimeEndpoint    = "/run/criproxy.sock"
	proxyStopTimeoutSeconds = 5
	confFileMode            = 0600
	confDirMode             = 0700
)

var kubeletSettingsForCriProxy map[string]interface{} = map[string]interface{}{
	"containerRuntime":      "remote",
	"enableCRI":             true,
	"remoteRuntimeEndpoint": proxyRuntimeEndpoint,
	"remoteImageEndpoint":   proxyRuntimeEndpoint,
}

func loadJson(baseUrl, suffix string) (map[string]interface{}, error) {
	url := strings.TrimSuffix(baseUrl, "/") + suffix
	glog.V(1).Infof("Loading url %q", url)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	res, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("trying to get %q: %v", url, err)
	}

	defer res.Body.Close()
	var r map[string]interface{}
	d := json.NewDecoder(res.Body)
	d.UseNumber() // avoid getting floats
	if err := d.Decode(&r); err != nil {
		return nil, fmt.Errorf("failed to unmarshal json from %q: %v", url, err)
	}
	return r, nil
}

func writeJson(data interface{}, file string) error {
	glog.V(1).Infof("Writing config file: %q", file)
	dir := path.Dir(file)
	if err := os.MkdirAll(dir, confDirMode); err != nil {
		return fmt.Errorf("failed to make conf dir: %v", err)
	}
	bs, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal json: %v", err)
	}
	if err := ioutil.WriteFile(file, bs, confFileMode); err != nil {
		return fmt.Errorf("error writing %q: %v", file, err)
	}
	return nil
}

type BootstrapConfig struct {
	ApiServerHost   string
	ConfigzBaseUrl  string
	StatsBaseUrl    string
	SavedConfigPath string
	ProxyPath       string
	ProxyArgs       []string
	ProxySocketPath string
}

type Bootstrap struct {
	config     *BootstrapConfig
	clientset  kubernetes.Interface
	kubeletCfg map[string]interface{}
}

// NewBootstrap creates a new Bootstrap object used for CRI proxy
// bootstrap using the specified BootstrapConfig. cs argument
// is used to pass a fake Clientset during tests, it should
// be nil when performing real bootstrap.
func NewBootstrap(config *BootstrapConfig, cs kubernetes.Interface) *Bootstrap {
	return &Bootstrap{config: config, clientset: cs}
}

func (b *Bootstrap) obtainKubeletConfig() error {
	cfg, err := loadJson(b.config.ConfigzBaseUrl, "/configz")
	if err != nil {
		return err
	}
	var ok bool
	b.kubeletCfg, ok = cfg["componentconfig"].(map[string]interface{})
	if !ok {
		return errors.New("couldn't get componentconfig from /configz")
	}
	return nil
}

func (b *Bootstrap) kubeletUpdated() bool {
	for k, v := range kubeletSettingsForCriProxy {
		if b.kubeletCfg[k] != v {
			return false
		}
	}
	return true
}

func (b *Bootstrap) updateKubeletConfig() {
	for k, v := range kubeletSettingsForCriProxy {
		b.kubeletCfg[k] = v
	}
}

func (b *Bootstrap) getNodeNameFromKubelet() (string, error) {
	stats, err := loadJson(b.config.StatsBaseUrl, "/stats/summary")
	if err != nil {
		return "", err
	}
	nodeProps, ok := stats["node"].(map[string]interface{})
	if !ok {
		return "", errors.New("couldn't get node properties /stats/summary")
	}
	nodeName, _ := nodeProps["nodeName"].(string)
	if nodeName == "" {
		return "", errors.New("couldn't get node name via /stats/summary")
	}
	return nodeName, nil
}

func (b *Bootstrap) buildConfigMap(nodeName string) *v1.ConfigMap {
	text, err := json.Marshal(b.kubeletCfg)
	if err != nil {
		log.Panicf("Couldn't marshal kubelet config: %v", err)
	}
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubelet-" + nodeName,
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"kubelet.config": string(text),
		},
	}
}

func (b *Bootstrap) putConfigMap(configMap *v1.ConfigMap) error {
	glog.V(1).Infof("Putting ConfigMap %q in namespace %q", configMap.Name, configMap.Namespace)
	_, err := b.clientset.Core().ConfigMaps("kube-system").Create(configMap)
	return err
}

func (b *Bootstrap) needToPatch() (bool, error) {
	_, err := os.Stat(b.config.SavedConfigPath)
	if err == nil {
		glog.V(1).Infof("Config file already exists: %q", b.config.SavedConfigPath)
		return false, nil
	}
	if os.IsNotExist(err) {
		return true, nil
	}
	return false, fmt.Errorf("failed to check for existing config: %v", err)
}

func (b *Bootstrap) dockerEndpoint() (string, error) {
	dockerEp, ok := b.kubeletCfg["dockerEndpoint"].(string)
	if !ok {
		return "", errors.New("failed to retrieve docker endpoint from kubelet config")
	}
	glog.V(1).Infof("Using docker endpoint %q", dockerEp)
	return dockerEp, nil
}

func (b *Bootstrap) saveKubeletConfig() error {
	return writeJson(b.kubeletCfg, b.config.SavedConfigPath)
}

func (b *Bootstrap) deleteSavedKubeletConfig() {
	if b.config.SavedConfigPath == "" {
		return
	}
	if err := os.Remove(b.config.SavedConfigPath); err != nil {
		glog.Errorf("Failed to remove saved kubelet config: %q", b.config.SavedConfigPath)
	}
}

func (b *Bootstrap) patchKubeletConfig() error {
	if b.kubeletUpdated() {
		return fmt.Errorf("kubelet already configured for CRI, but no saved config")
	}
	b.updateKubeletConfig()

	nodeName, err := b.getNodeNameFromKubelet()
	if err != nil {
		return err
	}
	glog.V(1).Infof("Node name: %q", nodeName)
	if err := b.putConfigMap(b.buildConfigMap(nodeName)); err != nil {
		return fmt.Errorf("failed to put ConfigMap: %v", err)
	}
	return nil
}

func (b *Bootstrap) installCriProxyContainer(dockerEndpoint, endpointToPass string) (string, error) {
	// CRI proxy container actually uses host namespaces to run.
	// It just uses docker's 'always' restart policy as a poor man's
	// substitute for a process manager.
	ctx := context.Background()

	client, err := dockerclient.NewClient(dockerEndpoint, "", nil, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create Docker client: %v", err)
	}

	filterArgs := dockerfilters.NewArgs()
	filterArgs.Add("label", "criproxy")
	containers, err := client.ContainerList(ctx, dockertypes.ContainerListOptions{
		Filter: filterArgs,
	})
	if len(containers) > 0 {
		for _, container := range containers {
			glog.V(1).Infof("Removing old CRI proxy container %s", container.ID)
			if err := client.ContainerRemove(ctx, container.ID, dockertypes.ContainerRemoveOptions{
				Force: true,
			}); err != nil {
				return "", fmt.Errorf("failed to remove old container: %v", err)
			}
		}
	}

	if err := pullImage(ctx, client, BusyboxImageName, true); err != nil {
		return "", fmt.Errorf("failed to pull busybox image: %v", err)
	}

	containerName := fmt.Sprintf("criproxy-%d", time.Now().UnixNano())
	resp, err := client.ContainerCreate(ctx, &dockercontainer.Config{
		Image:  BusyboxImageName,
		Labels: map[string]string{"criproxy": "true"},
		Env:    []string{"DOCKER_HOST=" + endpointToPass},
		Cmd: append([]string{
			"nsenter",
			"--mount=/proc/1/ns/mnt",
			"--",
			b.config.ProxyPath,
		}, b.config.ProxyArgs...),
	}, &dockercontainer.HostConfig{
		Privileged:  true,
		NetworkMode: "host",
		UTSMode:     "host",
		PidMode:     "host",
		IpcMode:     "host",
		UsernsMode:  "host",
		RestartPolicy: dockercontainer.RestartPolicy{
			Name: "always",
		},
	}, nil, containerName)
	if err != nil {
		return "", fmt.Errorf("failed to create CRI proxy container: %v", err)
	}
	if err := client.ContainerStart(ctx, resp.ID); err != nil {
		client.ContainerRemove(ctx, resp.ID, dockertypes.ContainerRemoveOptions{
			Force: true,
		})
		return "", fmt.Errorf("failed to start CRI proxy container: %v", err)
	}
	glog.Infof("Started container %s", resp.ID)
	return resp.ID, nil
}

func (b *Bootstrap) initClientset() error {
	var err error
	var config *rest.Config
	if b.config.ApiServerHost == "" {
		config, err = rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("failed to get REST client config: %v", err)
		}
	} else {
		config = &rest.Config{Host: b.config.ApiServerHost}
	}
	b.clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create ClientSet: %v", err)
	}
	return nil
}

// EnsureCRIProxy checks whether kubelet configuration file exists
// and performs CRI proxy bootstrap procedure if it doesn't.
func (b *Bootstrap) EnsureCRIProxy() (bool, error) {
	if b.config.ConfigzBaseUrl == "" || b.config.StatsBaseUrl == "" || b.config.ProxyPath == "" || b.config.ProxySocketPath == "" {
		return false, errors.New("invalid BootstrapConfig")
	}

	if !strings.HasPrefix(path.Clean(b.config.ProxySocketPath), "/run/") {
		return false, fmt.Errorf("proxy socket path %q not under /run", b.config.ProxySocketPath)
	}

	if needToPatch, err := b.needToPatch(); !needToPatch || err != nil {
		glog.V(1).Infof("Kubelet configuration already patched, exiting")
		return false, err
	}

	if err := b.obtainKubeletConfig(); err != nil {
		return false, err
	}

	// XXX FIX THIS!!!!
	// ONLY FOR DIND!!!
	b.kubeletCfg["enforceNodeAllocatable"] = []string{}

	// Need to have kubelet config saved at this point
	// so it can be used by the container below.
	if err := b.saveKubeletConfig(); err != nil {
		return false, err
	}

	ok := false
	defer func() {
		if !ok {
			b.deleteSavedKubeletConfig()
		}
	}()

	dockerEndpoint, err := b.dockerEndpoint()
	if err != nil {
		return false, err
	}
	if _, err = b.installCriProxyContainer(dockerEndpoint, dockerEndpoint); err != nil {
		return false, err
	}

	if err = waitForSocket(b.config.ProxySocketPath); err != nil {
		return false, err
	}

	// We don't try to patch kubelet config before the container
	// is installed, because this process may die with kubelet
	// restart
	if err := b.initClientset(); err != nil {
		return false, err
	}
	if err := b.patchKubeletConfig(); err != nil {
		return false, err
	}

	glog.V(1).Info("CRI proxy bootstrap complete")
	ok = true
	return true, nil
}

// LoadKubeletConfig loads kubelet configuration from a json file
func LoadKubeletConfig(path string) (*cfg.KubeletConfiguration, error) {
	bs, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg cfg.KubeletConfiguration
	if err := json.Unmarshal(bs, &cfg); err != nil {
		return nil, fmt.Errorf("failed to load kubelet config from %q: %v", path, err)
	}
	return &cfg, err
}
