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
	"fmt"

	dockerclient "github.com/docker/engine-api/client"
)

const (
	kubernetesPodNameLabel = "io.kubernetes.pod.name"
)

// RemoveKubernetesContainers kills kubernetes containers on the node so
// they get restarted.  FIXME: find the actual reason why kube-dns &
// kubernetes-dashboard fail after the restart.
func RemoveKubernetesContainers(dockerEndpoint string) error {
	client, err := dockerclient.NewClient(dockerEndpoint, "", nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %v", err)
	}

	ctx := context.Background()
	if err := removeContainersByLabels(ctx, client, []string{kubernetesPodNameLabel}, nil); err != nil {
		return fmt.Errorf("failed to remove the containers from plain docker runtime: %v", err)
	}

	return nil
}
