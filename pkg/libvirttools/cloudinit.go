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

package libvirttools

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"

	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/cni"
	"github.com/Mirantis/virtlet/pkg/flexvolume"
	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	EnvFileLocation = "/etc/cloud/environment"
)

type CloudInitGenerator struct {
	config    *VMConfig
	volumeMap map[string]string
	isoDir    string
}

func NewCloudInitGenerator(config *VMConfig, volumeMap map[string]string, isoDir string) *CloudInitGenerator {

	return &CloudInitGenerator{
		config:    config,
		volumeMap: volumeMap,
		isoDir:    isoDir,
	}
}

func (g *CloudInitGenerator) generateMetaData() ([]byte, error) {
	m := map[string]interface{}{
		"instance-id":    fmt.Sprintf("%s.%s", g.config.PodName, g.config.PodNamespace),
		"local-hostname": g.config.PodName,
	}
	if len(g.config.ParsedAnnotations.SSHKeys) != 0 {
		var keys []string
		for _, key := range g.config.ParsedAnnotations.SSHKeys {
			keys = append(keys, key)
		}
		m["public-keys"] = keys
	}
	for k, v := range g.config.ParsedAnnotations.MetaData {
		m[k] = v
	}
	r, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("error marshaling meta-data: %v", err)
	}
	return r, nil
}

func (g *CloudInitGenerator) generateUserData() ([]byte, error) {
	if g.config.ParsedAnnotations.UserDataScript != "" {
		return []byte(g.config.ParsedAnnotations.UserDataScript), nil
	}

	userData := make(map[string]interface{})
	for k, v := range g.config.ParsedAnnotations.UserData {
		userData[k] = v
	}

	g.addEnvVarsFileToWriteFiles(userData)

	writeFilesManipulator := NewWriteFilesManipulator(userData, g.config.Mounts)
	writeFilesManipulator.AddSecrets()
	writeFilesManipulator.AddConfigMapEntries()
	writeFilesManipulator.AddFileLikeMounts()

	mounts := utils.Merge(userData["mounts"], g.generateMounts()).([]interface{})
	if len(mounts) > 0 {
		userData["mounts"] = mounts
	}

	r := []byte{}
	if len(userData) != 0 {
		var err error
		r, err = yaml.Marshal(userData)
		if err != nil {
			return nil, fmt.Errorf("error marshalling user-data: %v", err)
		}
	}
	return []byte("#cloud-config\n" + string(r)), nil
}

func (g *CloudInitGenerator) generateNetworkConfiguration() ([]byte, error) {
	cniResult, err := cni.BytesToResult([]byte(g.config.CNIConfig))
	if err != nil {
		return nil, err
	}
	if cniResult == nil {
		// This can only happen during integration tests
		// where a dummy sandbox is used
		return []byte("version: 1\n"), nil
	}

	var config []map[string]interface{}
	var gateways []net.IP

	// physical interfaces
	for i, iface := range cniResult.Interfaces {
		if iface.Sandbox == "" {
			// skip host interfaces
			continue
		}
		subnets, curGateways := g.getSubnetsAndGatewaysForNthInterface(i, cniResult)
		gateways = append(gateways, curGateways...)
		interfaceConf := map[string]interface{}{
			"type":        "physical",
			"name":        iface.Name,
			"mac_address": iface.Mac,
			"subnets":     subnets,
		}
		config = append(config, interfaceConf)
	}

	// routes
	gotDefault := false
	for _, cniRoute := range cniResult.Routes {
		gw := cniRoute.GW
		switch {
		case gw != nil:
			// ok
		case len(gateways) == 0:
			glog.Warning("cloud-init: no gateways specified but got a route with empty gateway")
			continue
		case len(gateways) > 1:
			gw = gateways[0]
			glog.Warning("cloud-init: got more than one gateway and a route with empty gateway, using the first gateway: %q", gw)
		default:
			gw = gateways[0]
		}
		if ones, _ := cniRoute.Dst.Mask.Size(); ones == 0 {
			if gotDefault {
				glog.Warning("cloud-init: got more than one default route, using only the first one")
				continue
			}
			gotDefault = true
		}
		route := map[string]interface{}{
			"type":        "route",
			"destination": cniRoute.Dst.String(),
			"gateway":     gw.String(),
		}
		config = append(config, route)
	}

	r, err := yaml.Marshal(map[string]interface{}{
		"config": config,
	})
	if err != nil {
		return nil, err
	}
	return []byte("version: 1\n" + string(r)), nil
}

func (g *CloudInitGenerator) getSubnetsAndGatewaysForNthInterface(interfaceNo int, cniResult *cnicurrent.Result) ([]map[string]interface{}, []net.IP) {
	var subnets []map[string]interface{}
	var gateways []net.IP
	for _, ipConfig := range cniResult.IPs {
		if ipConfig.Interface == interfaceNo {
			subnet := map[string]interface{}{
				"type":    "static",
				"address": ipConfig.Address.String(),
			}
			if !ipConfig.Gateway.IsUnspecified() {
				gateways = append(gateways, ipConfig.Gateway)
				// Note that we can't use ipConfig.Gateway as
				// subnet["gateway"] because according CNI spec,
				// it must not be used to produce any routes by itself.
				// The routes must be specified in Routes field
				// of the CNI result.
			}
			// Cloud Init requires dns settings on the subnet level (yeah, I know...)
			// while we get just one setting for all the IP configurations from CNI,
			// so as a workaround we're adding it to all subnets configurations
			if cniResult.DNS.Nameservers != nil {
				subnet["dns_nameservers"] = cniResult.DNS.Nameservers
			}
			if cniResult.DNS.Search != nil {
				subnet["dns_search"] = cniResult.DNS.Search
			}
			subnets = append(subnets, subnet)
		}
	}

	// fallback to dhcp - should never happen, we always should have IPs
	if subnets == nil {
		subnets = append(subnets, map[string]interface{}{
			"type": "dhcp",
		})
	}

	return subnets, gateways
}

func (g *CloudInitGenerator) IsoPath() string {
	return filepath.Join(g.isoDir, fmt.Sprintf("nocloud-%s.iso", g.config.DomainUUID))
}

func (g *CloudInitGenerator) GenerateDisk() (*libvirtxml.DomainDisk, error) {
	tmpDir, err := ioutil.TempDir("", "nocloud-")
	if err != nil {
		return nil, fmt.Errorf("can't create temp dir for nocloud: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	var metaData, userData, networkConfiguration []byte
	metaData, err = g.generateMetaData()
	if err == nil {
		userData, err = g.generateUserData()
	}
	if err == nil {
		networkConfiguration, err = g.generateNetworkConfiguration()
	}
	if err != nil {
		return nil, err
	}

	if err := utils.WriteFiles(tmpDir, map[string][]byte{
		"user-data":      userData,
		"meta-data":      metaData,
		"network-config": networkConfiguration,
	}); err != nil {
		return nil, fmt.Errorf("can't write user-data: %v", err)
	}

	if err := os.MkdirAll(g.isoDir, 0777); err != nil {
		return nil, fmt.Errorf("error making iso directory %q: %v", g.isoDir, err)
	}

	if err := utils.GenIsoImage(g.IsoPath(), "cidata", tmpDir); err != nil {
		if rmErr := os.Remove(g.IsoPath()); rmErr != nil {
			glog.Warningf("Error removing iso file %s: %v", g.IsoPath(), rmErr)
		}
		return nil, fmt.Errorf("error generating iso image: %v", err)
	}

	diskDef := &libvirtxml.DomainDisk{
		Type:     "file",
		Device:   "cdrom",
		Driver:   &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
		Source:   &libvirtxml.DomainDiskSource{File: g.IsoPath()},
		ReadOnly: &libvirtxml.DomainDiskReadOnly{},
	}
	return diskDef, nil
}

func (g *CloudInitGenerator) generateEnvVarsContent() string {
	var buffer bytes.Buffer
	for _, entry := range g.config.Environment {
		buffer.WriteString(fmt.Sprintf("%s=%s\n", entry.Key, entry.Value))
	}

	return buffer.String()
}

func (g *CloudInitGenerator) addEnvVarsFileToWriteFiles(userData map[string]interface{}) {
	content := g.generateEnvVarsContent()
	if content == "" {
		return
	}

	// TODO: use merge algorithm instead
	var oldWriteFiles []interface{}
	oldWriteFilesRaw, _ := userData["write_files"]
	if oldWriteFilesRaw != nil {
		var ok bool
		oldWriteFiles, ok = oldWriteFilesRaw.([]interface{})
		if !ok {
			glog.Warning("Malformed write_files entry in user-data, can't add env vars")
			return
		}
	}

	userData["write_files"] = append(oldWriteFiles, map[string]interface{}{
		"path":    EnvFileLocation,
		"content": content,
	})
}

func (g *CloudInitGenerator) generateMounts() []interface{} {
	var r []interface{}
	for _, m := range g.config.Mounts {
		uuid, part, err := flexvolume.GetFlexvolumeInfo(m.HostPath)
		if err != nil {
			glog.Errorf("Can't mount directory %q to %q inside the VM: can't get flexvolume uuid: %v", m.HostPath, m.ContainerPath, err)
			continue
		}
		devPath, found := g.volumeMap[uuid]
		if !found {
			glog.Errorf("Can't mount directory %q to %q inside the VM: no device found for flexvolume uuid %q", m.HostPath, m.ContainerPath, uuid)
			continue
		}
		if part < 0 {
			part = 1
		}
		if part != 0 {
			devPath = fmt.Sprintf("%s%d", devPath, part)
		}
		r = append(r, []interface{}{devPath, m.ContainerPath})
	}
	return r
}

type WriteFilesManipulator struct {
	userData map[string]interface{}
	mounts   []*VMMount
}

func NewWriteFilesManipulator(userData map[string]interface{}, mounts []*VMMount) *WriteFilesManipulator {
	return &WriteFilesManipulator{
		userData: userData,
		mounts:   mounts,
	}
}

func (m *WriteFilesManipulator) AddSecrets() {
	m.addFilesForVolumeType("secret")
}

func (m *WriteFilesManipulator) AddConfigMapEntries() {
	m.addFilesForVolumeType("configmap")
}

func (m *WriteFilesManipulator) AddFileLikeMounts() {
	m.processMounts(func(path string) bool {
		fi, err := os.Stat(path)
		switch {
		case err != nil:
			return false
		case fi.Mode().IsRegular():
			return true
		}
		return false
	}, func(mount *VMMount) []interface{} {
		content, err := ioutil.ReadFile(mount.HostPath)
		if err != nil {
			glog.Warningf("Error during reading content of '%s' file: %v", mount.HostPath, err)
			return nil
		}

		glog.V(3).Infof("Adding file '%s' as volume: %s", mount.HostPath, mount.ContainerPath)
		encodedContent := base64.StdEncoding.EncodeToString(content)
		return []interface{}{map[string]interface{}{
			"path":        mount.ContainerPath,
			"content":     encodedContent,
			"encoding":    "b64",
			"permissions": "0644",
		}}
	})
}

func (m *WriteFilesManipulator) AddNetworkConfiguration() {
}

func (m *WriteFilesManipulator) addFilesForVolumeType(suffix string) {
	filter := "volumes/kubernetes.io~" + suffix + "/"

	m.processMounts(func(path string) bool {
		return strings.Contains(path, filter)
	}, func(mount *VMMount) []interface{} {
		return m.addFilesForMount(mount)
	})
}

func (m *WriteFilesManipulator) processMounts(filter func(string) bool, generateEntries func(*VMMount) []interface{}) {
	var oldWriteFiles []interface{}
	oldWriteFilesRaw, _ := m.userData["write_files"]
	if oldWriteFilesRaw != nil {
		var ok bool
		oldWriteFiles, ok = oldWriteFilesRaw.([]interface{})
		if !ok {
			glog.Warning("Malformed write_files entry in user-data, can't add new entries")
			return
		}
	}

	var writeFiles []interface{}
	for _, mount := range m.mounts {
		if !filter(mount.HostPath) {
			continue
		}
		entries := generateEntries(mount)
		writeFiles = append(writeFiles, entries...)
	}
	if writeFiles != nil {
		m.userData["write_files"] = append(oldWriteFiles, writeFiles...)
	}
}

func scanDirectory(dirPath string, callback func(string) error) error {
	entries, err := ioutil.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		fullPath := path.Join(dirPath, entry.Name())

		switch {
		case entry.Mode().IsDir():
			glog.V(3).Infof("Scanning directory: %s", entry.Name())
			if err := scanDirectory(fullPath, callback); err != nil {
				return err
			}
			glog.V(3).Infof("Leaving directory: %s", entry.Name())
		case entry.Mode().IsRegular():
			glog.V(3).Infof("Found regular file: %s", entry.Name())
			err := callback(fullPath)
			if err != nil {
				return err
			}
			continue
		case entry.Mode()&os.ModeSymlink != 0:
			glog.V(3).Infof("Found symlink: %s", entry.Name())
			fi, err := os.Stat(fullPath)
			switch {
			case err != nil:
				return err
			case fi.Mode().IsRegular():
				err = callback(fullPath)
				if err != nil {
					return err
				}
			case fi.Mode().IsDir():
				glog.V(3).Info("... which points to directory, going deeper ...")

				// NOTE: this does not need to be protected against loops
				// because it's prepared by kubelet in safe manner (if it's not
				// it's bug on kubelet side
				if err := scanDirectory(fullPath, callback); err != nil {
					return err
				}
				glog.V(3).Infof("... came back from symlink to directory: %s", entry.Name())
			default:
				glog.V(3).Info("... but it's pointing to something other than directory or regular file")
			}
		}
	}

	return nil
}

func (m *WriteFilesManipulator) addFilesForMount(mount *VMMount) []interface{} {
	var writeFiles []interface{}

	addFileContent := func(fullPath string) error {
		content, err := ioutil.ReadFile(fullPath)
		if err != nil {
			return err
		}
		stat, err := os.Stat(fullPath)
		if err != nil {
			return err
		}
		relativePath := fullPath[len(mount.HostPath)+1:]

		encodedContent := base64.StdEncoding.EncodeToString(content)
		writeFiles = append(writeFiles, map[string]interface{}{
			"path":        path.Join(mount.ContainerPath, relativePath),
			"content":     encodedContent,
			"encoding":    "b64",
			"permissions": fmt.Sprintf("%#o", uint32(stat.Mode())),
		})

		return nil
	}

	glog.V(3).Infof("Scanning %s for files", mount.HostPath)
	if err := scanDirectory(mount.HostPath, addFileContent); err != nil {
		glog.Errorf("Error while scanning directory %s: %v", mount.HostPath, err)
	}
	glog.V(3).Infof("Found %d entries", len(writeFiles))

	return writeFiles
}
