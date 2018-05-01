package criapi

import (
	"github.com/Mirantis/virtlet/pkg/utils"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
	"strconv"
)

const (
	samplePodNsUuid       = "cd6bc1b7-a9e2-4739-ade9-3e2447d28a90"
	sampleContainerNsUuid = "a51f5bed-db9c-49b1-a1b6-9989d46d637b"
)

type ContainerTestConfig struct {
	Name                string
	SandboxId           string
	ContainerId         string
	Image               string
	RootImageVolumeName string
	Labels              map[string]string
	Annotations         map[string]string
}

func GetSandboxes(sandboxCount int) []*kubeapi.PodSandboxConfig {
	sandboxes := []*kubeapi.PodSandboxConfig{}
	for i := 0; i < sandboxCount; i++ {
		name := "testName_" + strconv.Itoa(i)

		namespace := "default"
		attempt := uint32(0)
		metadata := &kubeapi.PodSandboxMetadata{
			Name:      name,
			Uid:       utils.NewUUID5(samplePodNsUuid, name),
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
		name := "container-for-" + sandbox.Metadata.Name
		containerConf := &ContainerTestConfig{
			Name:                name,
			SandboxId:           sandbox.Metadata.Uid,
			Image:               "testImage",
			RootImageVolumeName: "sample_name",
			ContainerId:         utils.NewUUID5(sampleContainerNsUuid, name),
			Labels:              map[string]string{"foo": "bar", "fizz": "buzz"},
			Annotations:         map[string]string{"hello": "world", "virt": "let"},
		}
		containers = append(containers, containerConf)
	}

	return containers
}
