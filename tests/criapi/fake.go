package criapi

import (
	virtletutils "github.com/Mirantis/virtlet/pkg/utils"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
	"strconv"
)

type ContainerTestConfigSet struct {
	SandboxId             string
	ContainerId           string
	Image                 string
	RootImageSnapshotPath string
	Labels                map[string]string
	Annotations           map[string]string
}

func GetSandboxes(sandboxNum int) ([]*kubeapi.PodSandboxConfig, error) {
	sandboxes := []*kubeapi.PodSandboxConfig{}

	for i := 0; i < sandboxNum; i++ {
		name := "testName_" + strconv.Itoa(i)
		uid, err := virtletutils.NewUuid()

		if err != nil {
			return nil, err
		}

		namespace := "default"
		attempt := uint32(0)
		metadata := &kubeapi.PodSandboxMetadata{
			Name:      &name,
			Uid:       &uid,
			Namespace: &namespace,
			Attempt:   &attempt,
		}

		hostNetwork := false
		hostPid := false
		hostIpc := false
		namespaceOptions := &kubeapi.NamespaceOption{
			HostNetwork: &hostNetwork,
			HostPid:     &hostPid,
			HostIpc:     &hostIpc,
		}

		cgroupParent := ""
		linuxSandbox := &kubeapi.LinuxPodSandboxConfig{
			CgroupParent:     &cgroupParent,
			NamespaceOptions: namespaceOptions,
		}

		hostname := "localhost"
		logDirectory := "/var/log/test_log_directory"
		sandboxConfig := &kubeapi.PodSandboxConfig{
			Metadata:     metadata,
			Hostname:     &hostname,
			LogDirectory: &logDirectory,
			Labels: map[string]string{
				"foo":  "bar",
				"fizz": "buzz",
			},
			Annotations: map[string]string{
				"hello": "world",
				"virt":  "let",
			},
			Linux: linuxSandbox,
		}

		sandboxes = append(sandboxes, sandboxConfig)
	}

	return sandboxes, nil
}

func GetContainersConfig(sandboxConfigs []*kubeapi.PodSandboxConfig) ([]*ContainerTestConfigSet, error) {
	containers := []*ContainerTestConfigSet{}

	for _, sandbox := range sandboxConfigs {
		uid, err := virtletutils.NewUuid()

		if err != nil {
			return nil, err
		}
		containerConf := &ContainerTestConfigSet{
			SandboxId: *sandbox.Metadata.Uid,
			Image:     "testImage",
			RootImageSnapshotPath: "/sample/path",
			ContainerId:           uid,
			Labels:                map[string]string{"foo": "bar", "fizz": "buzz"},
			Annotations:           map[string]string{"hello": "world", "virt": "let"},
		}
		containers = append(containers, containerConf)
	}

	return containers, nil
}
