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
	"encoding/json"
	"fmt"
	"io"

	// TODO: switch to https://github.com/docker/docker/tree/master/client
	// Docker version used in k8s is too old for it
	dockermessage "github.com/docker/docker/pkg/jsonmessage"
	dockerclient "github.com/docker/engine-api/client"
	dockertypes "github.com/docker/engine-api/types"
	dockerfilters "github.com/docker/engine-api/types/filters"

	"github.com/golang/glog"
)

func removeContainersByLabels(ctx context.Context, client *dockerclient.Client, labels []string, filter func(c dockertypes.Container) bool) error {
	filterArgs := dockerfilters.NewArgs()
	for _, label := range labels {
		filterArgs.Add("label", label)
	}
	containers, err := client.ContainerList(ctx, dockertypes.ContainerListOptions{
		Filter: filterArgs,
		All:    true,
	})
	if err != nil {
		return fmt.Errorf("error listing containers: %v", err)
	}
	if len(containers) > 0 {
		for _, container := range containers {
			if filter != nil && !filter(container) {
				continue
			}
			glog.V(1).Infof("Stopping docker container %s (labels: %#v)", container.ID, container.Labels)
			if err := client.ContainerStop(ctx, container.ID, 1); err != nil {
				return fmt.Errorf("failed to stop container: %v", err)
			}
		}
	}
	return nil
}

func pullImage(ctx context.Context, client *dockerclient.Client, imageName string, print bool) error {
	glog.V(1).Infof("Pulling image %q", imageName)
	resp, err := client.ImagePull(ctx, imageName, dockertypes.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("Failed to pull busybox image: %v", err)
	}

	decoder := json.NewDecoder(resp)
	for {
		var msg dockermessage.JSONMessage
		err := decoder.Decode(&msg)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error decoding docker message: %v", err)
		}
		if msg.Error != nil {
			return msg.Error
		}
		if print {
			fmt.Println(msg.Status)
		}
	}
	return nil
}
