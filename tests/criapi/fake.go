package criapi

import (
	"github.com/Mirantis/virtlet/pkg/utils"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"
	"strconv"
)

const (
	samplePodNsUUID       = "cd6bc1b7-a9e2-4739-ade9-3e2447d28a90"
	sampleContainerNsUUID = "a51f5bed-db9c-49b1-a1b6-9989d46d637b"
)

// ContainerTestConfig specifies configuration for a container test.
type ContainerTestConfig struct {
	// Container name.
	Name string
	// Pod sandbox id.
	SandboxID string
	// Container id.
	ContainerID string
	// Image reference.
	Image string
	// Container labels.
	Labels map[string]string
	// Container annotations.
	Annotations map[string]string
}

// GetSandboxes returns the specified number of PodSandboxConfig
// objects with "fake" contents.
func GetSandboxes(sandboxCount int) []*kubeapi.PodSandboxConfig {
	sandboxes := []*kubeapi.PodSandboxConfig{}
	for i := 0; i < sandboxCount; i++ {
		name := "testName_" + strconv.Itoa(i)

		namespace := "default"
		attempt := uint32(0)
		metadata := &kubeapi.PodSandboxMetadata{
			Name:      name,
			Uid:       utils.NewUUID5(samplePodNsUUID, name),
			Namespace: namespace,
			Attempt:   attempt,
		}

		namespaceOptions := &kubeapi.NamespaceOption{
			Network: kubeapi.NamespaceMode_POD,
			Pid:     kubeapi.NamespaceMode_POD,
			Ipc:     kubeapi.NamespaceMode_POD,
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

// GetContainersConfig returns the specified number of
// ContainerTestConfig objects.
func GetContainersConfig(sandboxConfigs []*kubeapi.PodSandboxConfig) []*ContainerTestConfig {
	containers := []*ContainerTestConfig{}
	for _, sandbox := range sandboxConfigs {
		name := "container-for-" + sandbox.Metadata.Name
		containerConf := &ContainerTestConfig{
			Name:        name,
			SandboxID:   sandbox.Metadata.Uid,
			Image:       "testImage",
			ContainerID: utils.NewUUID5(sampleContainerNsUUID, name),
			Labels:      map[string]string{"foo": "bar", "fizz": "buzz"},
			Annotations: map[string]string{"hello": "world", "virt": "let"},
		}
		containers = append(containers, containerConf)
	}

	return containers
}
