package libvirttools

import (
	"fmt"
	"os"
	"path"
	"strings"
	"syscall"

	"github.com/golang/glog"
	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
)

const (
	mountTagfor9pfs       = "virtletShared"
	mountPointPathfor9pfs = "/virtletShared"
)

type filesystemItem struct {
	mount            types.VMMount
	volumeMountPoint string
}

type filesystemList struct {
	config         *types.VMConfig
	items          []*filesystemItem
	podVolumesPath string
}

func generateMountTag(hostPath string) string {
	return fmt.Sprintf("volume-%s", path.Base(hostPath))
}

func newFilesystemList(config *types.VMConfig, vConfig VirtualizationConfig) (*filesystemList, error) {
	volumePoolPath := supportedStoragePools[vConfig.VolumePoolName]
	if _, err := os.Stat(volumePoolPath); err != nil {
		return nil, err
	}
	podVolumesPath := path.Join(volumePoolPath, config.DomainUUID)
	err := os.MkdirAll(podVolumesPath, 0777)
	if err != nil {
		glog.Errorf("Failed to create podVolumesPath: %v", err)
		return nil, err
	}
	err = ChownForEmulator(podVolumesPath, true)
	if err != nil {
		glog.Errorf("Failed to Change owner of podVolumesPath to emulator: %v", err)
		return nil, err
	}

	var items []*filesystemItem
	for _, m := range config.Mounts {
		if isRegularFile(m.HostPath) ||
			strings.Contains(m.HostPath, flexvolumeSubdir) ||
			strings.Contains(m.HostPath, "kubernetes.io~secret") ||
			strings.Contains(m.HostPath, "kubernetes.io~configmap") {
			continue
		}

		volumeMountPoint := path.Join(podVolumesPath, path.Base(m.HostPath))
		items = append(items, &filesystemItem{m, volumeMountPoint})
	}
	return &filesystemList{config, items, podVolumesPath}, nil
}

func (fsList *filesystemList) setup() ([]libvirtxml.DomainFilesystem, error) {
	var filesystems []libvirtxml.DomainFilesystem
	for n, item := range fsList.items {
		err := item.setup()
		if err != nil {
			// try to tear down volumes that were already set up
			for _, item := range fsList.items[:n] {
				if err := item.teardown(); err != nil {
					glog.Warningf("Failed to tear down a fs volume on error: %v", err)
				}
			}
			if err := os.RemoveAll(fsList.podVolumesPath); err != nil {
				glog.Warningf("failed to remove '%s': %v", fsList.podVolumesPath)
			}

			return nil, err
		}
	}

	fsDef := &libvirtxml.DomainFilesystem{
		AccessMode: "squash",
		Source:     &libvirtxml.DomainFilesystemSource{Mount: &libvirtxml.DomainFilesystemSourceMount{Dir: fsList.podVolumesPath}},
		Target:     &libvirtxml.DomainFilesystemTarget{Dir: mountTagfor9pfs},
	}
	filesystems = append(filesystems, *fsDef)
	return filesystems, nil
}

func (fsList *filesystemList) teardown() error {
	var errs []string
	for _, item := range fsList.items {
		if err := item.teardown(); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if err := os.RemoveAll(fsList.podVolumesPath); err != nil {
		errs = append(errs, err.Error())
	}
	if errs != nil {
		return fmt.Errorf("failed to tear down some of the fs volumes:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

func (fsItem *filesystemItem) setup() error {
	err := os.MkdirAll(fsItem.volumeMountPoint, 0777)
	if err == nil {
		err = ChownForEmulator(fsItem.volumeMountPoint, true)
	}
	if err == nil {
		err = syscall.Mount(fsItem.mount.HostPath, fsItem.volumeMountPoint, "bind", syscall.MS_BIND|syscall.MS_REC, "")
	}
	if err == nil {
		err = ChownForEmulator(fsItem.volumeMountPoint, true)
	}
	if err != nil {
		glog.Errorf("Failed to create vm pod path: %v", err)
		return err
	}

	return nil
}

func (fsItem *filesystemItem) teardown() error {
	var err error
	if _, err = os.Stat(fsItem.volumeMountPoint); err == nil {
		err = syscall.Unmount(fsItem.volumeMountPoint, 0)
	}
	if err == nil {
		err = os.RemoveAll(fsItem.volumeMountPoint)
	}
	if err != nil {
		return fmt.Errorf("failed to tear down fs volume mountpoint '%s': %v", fsItem.volumeMountPoint, err)
	}

	return nil
}
