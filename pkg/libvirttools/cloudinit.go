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
	"strconv"
	"strings"

	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/flexvolume"
	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/network"
	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	envFileLocation   = "/etc/cloud/environment"
	mountFileLocation = "/etc/cloud/mount-volumes.sh"
	mountScriptSubst  = "@virtlet-mount-script@"
)

// CloudInitGenerator provides a common part for Cloud Init ISO drive preparation
// for NoCloud and ConfigDrive volume sources.
type CloudInitGenerator struct {
	config *types.VMConfig
	isoDir string
}

// NewCloudInitGenerator returns new CloudInitGenerator.
func NewCloudInitGenerator(config *types.VMConfig, isoDir string) *CloudInitGenerator {
	return &CloudInitGenerator{
		config: config,
		isoDir: isoDir,
	}
}

func (g *CloudInitGenerator) generateMetaData() ([]byte, error) {
	m := map[string]interface{}{
		"instance-id":    fmt.Sprintf("%s.%s", g.config.PodName, g.config.PodNamespace),
		"local-hostname": g.config.PodName,
	}

	// TODO: get rid of this if. Use descriptor for cloud-init image types.
	if g.config.ParsedAnnotations.CDImageType == types.CloudInitImageTypeConfigDrive {
		m["uuid"] = m["instance-id"]
		m["hostname"] = m["local-hostname"]
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

func (g *CloudInitGenerator) generateUserData(volumeMap diskPathMap) ([]byte, error) {
	mounts, mountScript := g.generateMounts(volumeMap)

	if userDataScript := g.config.ParsedAnnotations.UserDataScript; userDataScript != "" {
		return []byte(strings.Replace(userDataScript, mountScriptSubst, mountScript, -1)), nil
	}

	userData := make(map[string]interface{})
	for k, v := range g.config.ParsedAnnotations.UserData {
		userData[k] = v
	}

	mounts = utils.Merge(userData["mounts"], mounts).([]interface{})
	if len(mounts) != 0 {
		userData["mounts"] = mounts
	}

	writeFilesUpdater := newWriteFilesUpdater(g.config.Mounts)
	writeFilesUpdater.addSecrets()
	writeFilesUpdater.addConfigMapEntries()
	writeFilesUpdater.addFileLikeMounts()
	if mountScript != "" {
		writeFilesUpdater.addMountScript(mountScript)
	}
	if envContent := g.generateEnvVarsContent(); envContent != "" {
		writeFilesUpdater.addEnvironmentFile(envContent)
	}
	writeFilesUpdater.updateUserData(userData)

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
	// TODO: get rid of this switch. Use descriptor for cloud-init image types.
	switch g.config.ParsedAnnotations.CDImageType {
	case types.CloudInitImageTypeNoCloud:
		return g.generateNetworkConfigurationNoCloud()
	case types.CloudInitImageTypeConfigDrive:
		return g.generateNetworkConfigurationConfigDrive()
	}

	return nil, fmt.Errorf("unknown cloud-init config image type: %q", g.config.ParsedAnnotations.CDImageType)
}

func (g *CloudInitGenerator) generateNetworkConfigurationNoCloud() ([]byte, error) {
	if g.config.ContainerSideNetwork == nil {
		// This can only happen during integration tests
		// where a dummy sandbox is used
		return []byte("version: 1\n"), nil
	}
	cniResult := g.config.ContainerSideNetwork.Result

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
		mtu, err := mtuForMacAddress(iface.Mac, g.config.ContainerSideNetwork.Interfaces)
		if err != nil {
			return nil, err
		}
		interfaceConf := map[string]interface{}{
			"type":        "physical",
			"name":        iface.Name,
			"mac_address": iface.Mac,
			"subnets":     subnets,
			"mtu":         mtu,
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
			glog.Warningf("cloud-init: got more than one gateway and a route with empty gateway, using the first gateway: %q", gw)
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

	// dns
	dnsData := getDNSData(cniResult.DNS)
	if dnsData != nil {
		config = append(config, dnsData...)
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
				"address": ipConfig.Address.IP.String(),
				"netmask": net.IP(ipConfig.Address.Mask).String(),
			}
			if !ipConfig.Gateway.IsUnspecified() {
				gateways = append(gateways, ipConfig.Gateway)
				// Note that we can't use ipConfig.Gateway as
				// subnet["gateway"] because according CNI spec,
				// it must not be used to produce any routes by itself.
				// The routes must be specified in Routes field
				// of the CNI result.
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

func getDNSData(cniDNS cnitypes.DNS) []map[string]interface{} {
	var dnsData []map[string]interface{}
	if cniDNS.Nameservers != nil {
		dnsData = append(dnsData, map[string]interface{}{
			"type":    "nameserver",
			"address": cniDNS.Nameservers,
		})
		if cniDNS.Search != nil {
			dnsData[0]["search"] = cniDNS.Search
		}
	}
	return dnsData
}

func (g *CloudInitGenerator) generateNetworkConfigurationConfigDrive() ([]byte, error) {
	if g.config.ContainerSideNetwork == nil {
		// This can only happen during integration tests
		// where a dummy sandbox is used
		return []byte("{}"), nil
	}
	cniResult := g.config.ContainerSideNetwork.Result

	config := make(map[string]interface{})

	// links
	var links []map[string]interface{}
	for _, iface := range cniResult.Interfaces {
		if iface.Sandbox == "" {
			// skip host interfaces
			continue
		}
		mtu, err := mtuForMacAddress(iface.Mac, g.config.ContainerSideNetwork.Interfaces)
		if err != nil {
			return nil, err
		}
		linkConf := map[string]interface{}{
			"type": "phy",
			"id":   iface.Name,
			"ethernet_mac_address": iface.Mac,
			"mtu": mtu,
		}
		links = append(links, linkConf)
	}
	config["links"] = links

	var networks []map[string]interface{}
	for i, ipConfig := range cniResult.IPs {
		netConf := map[string]interface{}{
			"id": fmt.Sprintf("net-%d", i),
			// config from openstack have as network_id network uuid
			"network_id": fmt.Sprintf("net-%d", i),
			"type":       fmt.Sprintf("ipv%s", ipConfig.Version),
			"link":       cniResult.Interfaces[ipConfig.Interface].Name,
			"ip_address": ipConfig.Address.IP.String(),
			"netmask":    net.IP(ipConfig.Address.Mask).String(),
		}

		routes := routesForIP(ipConfig.Address, cniResult.Routes)
		if routes != nil {
			netConf["routes"] = routes
		}

		networks = append(networks, netConf)
	}
	config["networks"] = networks

	dnsData := getDNSData(cniResult.DNS)
	if dnsData != nil {
		config["services"] = dnsData
	}

	r, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("error marshaling network configuration: %v", err)
	}
	return r, nil
}

func routesForIP(sourceIP net.IPNet, allRoutes []*cnitypes.Route) []map[string]interface{} {
	var routes []map[string]interface{}

	// NOTE: at the moment on cni result level there is no distinction
	// for which interface particular route should be set,
	// so we are returning there all routes with gateway accessible
	// by particular source ip address.
	for _, route := range allRoutes {
		if sourceIP.Contains(route.GW) {
			routes = append(routes, map[string]interface{}{
				"network": route.Dst.IP.String(),
				"netmask": net.IP(route.Dst.Mask).String(),
				"gateway": route.GW.String(),
			})
		}
	}

	return routes
}

func mtuForMacAddress(mac string, ifaces []*network.InterfaceDescription) (uint16, error) {
	for _, iface := range ifaces {
		if iface.HardwareAddr.String() == mac {
			return iface.MTU, nil
		}
	}
	return 0, fmt.Errorf("interface with mac address %q not found in ContainerSideNetwork", mac)
}

// IsoPath returns a full path to iso image with configuration for VM pod.
func (g *CloudInitGenerator) IsoPath() string {
	return filepath.Join(g.isoDir, fmt.Sprintf("config-%s.iso", g.config.DomainUUID))
}

// DiskDef returns a DomainDisk definition for Cloud Init ISO image to be included
// in VM pod libvirt domain definition.
func (g *CloudInitGenerator) DiskDef() *libvirtxml.DomainDisk {
	return &libvirtxml.DomainDisk{
		Device:   "cdrom",
		Driver:   &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
		Source:   &libvirtxml.DomainDiskSource{File: &libvirtxml.DomainDiskSourceFile{File: g.IsoPath()}},
		ReadOnly: &libvirtxml.DomainDiskReadOnly{},
	}
}

// GenerateImage collects metadata, userdata and network configuration and uses
// them to prepare an ISO image for NoCloud or ConfigDrive selecting the type
// using an info from pod annotations.
func (g *CloudInitGenerator) GenerateImage(volumeMap diskPathMap) error {
	tmpDir, err := ioutil.TempDir("", "config-")
	if err != nil {
		return fmt.Errorf("can't create temp dir for config image: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	var metaData, userData, networkConfiguration []byte
	metaData, err = g.generateMetaData()
	if err == nil {
		userData, err = g.generateUserData(volumeMap)
	}
	if err == nil {
		networkConfiguration, err = g.generateNetworkConfiguration()
	}
	if err != nil {
		return err
	}

	var userDataLocation, metaDataLocation, networkConfigLocation string
	var volumeName string

	// TODO: get rid of this switch. Use descriptor for cloud-init image types.
	switch g.config.ParsedAnnotations.CDImageType {
	case types.CloudInitImageTypeNoCloud:
		userDataLocation = "user-data"
		metaDataLocation = "meta-data"
		networkConfigLocation = "network-config"
		volumeName = "cidata"
	case types.CloudInitImageTypeConfigDrive:
		userDataLocation = "openstack/latest/user_data"
		metaDataLocation = "openstack/latest/meta_data.json"
		networkConfigLocation = "openstack/latest/network_data.json"
		volumeName = "config-2"
	default:
		// that should newer happen, as imageType should be validated
		// already earlier
		return fmt.Errorf("unknown cloud-init config image type: %q", g.config.ParsedAnnotations.CDImageType)
	}

	if err := utils.WriteFiles(tmpDir, map[string][]byte{
		userDataLocation:      userData,
		metaDataLocation:      metaData,
		networkConfigLocation: networkConfiguration,
	}); err != nil {
		return fmt.Errorf("can't write user-data: %v", err)
	}

	if err := os.MkdirAll(g.isoDir, 0777); err != nil {
		return fmt.Errorf("error making iso directory %q: %v", g.isoDir, err)
	}

	if err := utils.GenIsoImage(g.IsoPath(), volumeName, tmpDir); err != nil {
		if rmErr := os.Remove(g.IsoPath()); rmErr != nil {
			glog.Warningf("Error removing iso file %s: %v", g.IsoPath(), rmErr)
		}
		return fmt.Errorf("error generating iso image: %v", err)
	}

	return nil
}

func (g *CloudInitGenerator) generateEnvVarsContent() string {
	var buffer bytes.Buffer
	for _, entry := range g.config.Environment {
		buffer.WriteString(fmt.Sprintf("%s=%s\n", entry.Key, entry.Value))
	}

	return buffer.String()
}

func isRegularFile(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.Mode().IsRegular()
}

func (g *CloudInitGenerator) generateMounts(volumeMap diskPathMap) ([]interface{}, string) {
	var r []interface{}
	var mountScriptLines []string
	for _, m := range g.config.Mounts {
		// Skip file based mounts (including secrets and config maps).
		if isRegularFile(m.HostPath) ||
			strings.Contains(m.HostPath, "kubernetes.io~secret") ||
			strings.Contains(m.HostPath, "kubernetes.io~configmap") {
			continue
		}

		mountInfo, mountScriptLine, err := generateFlexvolumeMounts(volumeMap, m)
		if err != nil {
			if !os.IsNotExist(err) {
				glog.Errorf("Can't mount directory %q to %q inside the VM: %v", m.HostPath, m.ContainerPath, err)
				continue
			}

			// Fs based volume
			mountInfo, mountScriptLine, err = generateFsBasedVolumeMounts(m)
			if err != nil {
				glog.Errorf("Can't mount directory %q to %q inside the VM: %v", m.HostPath, m.ContainerPath, err)
				continue
			}
		}

		r = append(r, mountInfo)
		mountScriptLines = append(mountScriptLines, mountScriptLine)
	}

	mountScript := ""
	if len(mountScriptLines) != 0 {
		mountScript = fmt.Sprintf("#!/bin/sh\n%s\n", strings.Join(mountScriptLines, "\n"))
	}
	return r, mountScript
}

func generateFlexvolumeMounts(volumeMap diskPathMap, mount types.VMMount) ([]interface{}, string, error) {
	uuid, part, err := flexvolume.GetFlexvolumeInfo(mount.HostPath)
	if err != nil {
		// If the error is NotExist, return the original error
		if os.IsNotExist(err) {
			return nil, "", err
		}
		err = fmt.Errorf("can't get flexvolume uuid: %v", err)
		return nil, "", err
	}
	dpath, found := volumeMap[uuid]
	if !found {
		err = fmt.Errorf("no device found for flexvolume uuid %q", uuid)
		return nil, "", err
	}
	if part < 0 {
		part = 1
	}
	devPath := dpath.devPath
	mountDevSuffix := ""
	if part != 0 {
		devPath += fmt.Sprintf("-part%d", part)
		mountDevSuffix += strconv.Itoa(part)
	}
	// TODO: do better job at escaping mount.ContainerPath
	mountScriptLine := fmt.Sprintf("if ! mountpoint '%s'; then mkdir -p '%s' && mount /dev/`ls %s`%s '%s'; fi",
		mount.ContainerPath, mount.ContainerPath, dpath.sysfsPath, mountDevSuffix, mount.ContainerPath)
	return []interface{}{devPath, mount.ContainerPath}, mountScriptLine, nil
}

func generateFsBasedVolumeMounts(mount types.VMMount) ([]interface{}, string, error) {
	mountTag := path.Base(mount.ContainerPath)
	fsMountScript := fmt.Sprintf("if ! mountpoint '%s'; then mkdir -p '%s' && mount -t 9p -o trans=virtio %s '%s'; fi",
		mount.ContainerPath, mount.ContainerPath, mountTag, mount.ContainerPath)
	r := []interface{}{mountTag, mount.ContainerPath, "9p", "trans=virtio"}
	return r, fsMountScript, nil
}

type writeFilesUpdater struct {
	entries []interface{}
	mounts  []types.VMMount
}

func newWriteFilesUpdater(mounts []types.VMMount) *writeFilesUpdater {
	return &writeFilesUpdater{
		mounts: mounts,
	}
}

func (u *writeFilesUpdater) put(entry interface{}) {
	u.entries = append(u.entries, entry)
}

func (u *writeFilesUpdater) putPlainText(path string, content string, perms os.FileMode) {
	u.put(map[string]interface{}{
		"path":        path,
		"content":     content,
		"permissions": fmt.Sprintf("%#o", uint32(perms)),
	})
}

func (u *writeFilesUpdater) putBase64(path string, content []byte, perms os.FileMode) {
	encodedContent := base64.StdEncoding.EncodeToString(content)
	u.put(map[string]interface{}{
		"path":        path,
		"content":     encodedContent,
		"encoding":    "b64",
		"permissions": fmt.Sprintf("%#o", uint32(perms)),
	})
}

func (u *writeFilesUpdater) updateUserData(userData map[string]interface{}) {
	if len(u.entries) == 0 {
		return
	}

	writeFiles := utils.Merge(userData["write_files"], u.entries).([]interface{})
	if len(writeFiles) != 0 {
		userData["write_files"] = writeFiles
	}
}

func (u *writeFilesUpdater) addSecrets() {
	u.addFilesForVolumeType("secret")
}

func (u *writeFilesUpdater) addConfigMapEntries() {
	u.addFilesForVolumeType("configmap")
}

func (u *writeFilesUpdater) addFileLikeMounts() {
	for _, mount := range u.filterMounts(func(path string) bool {
		fi, err := os.Stat(path)
		switch {
		case err != nil:
			return false
		case fi.Mode().IsRegular():
			return true
		}
		return false
	}) {
		content, err := ioutil.ReadFile(mount.HostPath)
		if err != nil {
			glog.Warningf("Error during reading content of '%s' file: %v", mount.HostPath, err)
			continue
		}

		glog.V(3).Infof("Adding file '%s' as volume: %s", mount.HostPath, mount.ContainerPath)
		u.putBase64(mount.ContainerPath, content, 0644)
	}
}

func (u *writeFilesUpdater) addFilesForVolumeType(suffix string) {
	filter := "volumes/kubernetes.io~" + suffix + "/"
	for _, mount := range u.filterMounts(func(path string) bool {
		return strings.Contains(path, filter)
	}) {
		u.addFilesForMount(mount)
	}
}

func (u *writeFilesUpdater) addMountScript(content string) {
	u.putPlainText(mountFileLocation, content, 0755)
}

func (u *writeFilesUpdater) addEnvironmentFile(content string) {
	u.putPlainText(envFileLocation, content, 0644)
}

func (u *writeFilesUpdater) addFilesForMount(mount types.VMMount) []interface{} {
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
		u.putBase64(path.Join(mount.ContainerPath, relativePath), content, stat.Mode())
		return nil
	}

	glog.V(3).Infof("Scanning %s for files", mount.HostPath)
	if err := scanDirectory(mount.HostPath, addFileContent); err != nil {
		glog.Errorf("Error while scanning directory %s: %v", mount.HostPath, err)
	}
	glog.V(3).Infof("Found %d entries", len(writeFiles))

	return writeFiles
}

func (u *writeFilesUpdater) filterMounts(filter func(string) bool) []types.VMMount {
	var r []types.VMMount
	for _, mount := range u.mounts {
		if filter(mount.HostPath) {
			r = append(r, mount)
		}
	}
	return r
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
