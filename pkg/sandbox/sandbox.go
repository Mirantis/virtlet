/*
Copyright 2016 Mirantis

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

package sandbox

import (
	uuid "github.com/nu7hatch/gouuid"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/etcdtools"
)

func CreatePodSandbox(sandboxTool *etcdtools.SandboxTool, config *kubeapi.PodSandboxConfig) (string, error) {
	u, err := uuid.NewV4()
	if err != nil {
		return "", err
	}
	podId := u.String()

	if err := sandboxTool.CreatePodSandbox(podId, config); err != nil {
		return "", err
	}

	return podId, nil
}
