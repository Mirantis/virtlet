package criapi

import (
	"log"

	virtletutils "github.com/Mirantis/virtlet/pkg/utils"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
	"strconv"
)

type ContainerTestConfig struct {
	Name                  string
	SandboxId             string
	ContainerId           string
	Image                 string
	RootImageSnapshotName string
	Labels                map[string]string
	Annotations           map[string]string
}

func GetSandboxes(sandboxNum int) []*kubeapi.PodSandboxConfig {
	sandboxes := []*kubeapi.PodSandboxConfig{}

	for i := 0; i < sandboxNum; i++ {
		name := "testName_" + strconv.Itoa(i)
		uid, err := virtletutils.NewUuid()
		if err != nil {
			log.Panicf("NewUuid(): %v", err)
		}

		namespace := "default"
		attempt := uint32(0)
		metadata := &kubeapi.PodSandboxMetadata{
			Name:      name,
			Uid:       uid,
			Namespace: namespace,
			Attempt:   attempt,
		}

		hostNetwork := false
		hostPid := false
		hostIpc := false
		namespaceOptions := &kubeapi.NamespaceOption{
			HostNetwork: hostNetwork,
			HostPid:     hostPid,
			HostIpc:     hostIpc,
		}

		cgroupParent := ""
		linuxSandbox := &kubeapi.LinuxPodSandboxConfig{
			CgroupParent: cgroupParent,
			SecurityContext: &kubeapi.LinuxSandboxSecurityContext{
				NamespaceOptions: namespaceOptions,
			},
		}

		hostname := "localhost"
		logDirectory := "/var/log/test_log_directory"
		sandboxConfig := &kubeapi.PodSandboxConfig{
			Metadata:     metadata,
			Hostname:     hostname,
			LogDirectory: logDirectory,
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

	return sandboxes
}

func GetContainersConfig(sandboxConfigs []*kubeapi.PodSandboxConfig) []*ContainerTestConfig {
	containers := []*ContainerTestConfig{}
	for _, sandbox := range sandboxConfigs {
		uid, err := virtletutils.NewUuid()
		if err != nil {
			log.Panicf("NewUuid(): %v", err)
		}

		containerConf := &ContainerTestConfig{
			Name:      "container-for-" + sandbox.Metadata.Name,
			SandboxId: sandbox.Metadata.Uid,
			Image:     "testImage",
			RootImageSnapshotName: "sample_name",
			ContainerId:           uid,
			Labels:                map[string]string{"foo": "bar", "fizz": "buzz"},
			Annotations:           map[string]string{"hello": "world", "virt": "let"},
		}
		containers = append(containers, containerConf)
	}

	return containers
}
