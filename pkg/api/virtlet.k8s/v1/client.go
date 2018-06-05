/*
Copyright 2018 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or ≈git-agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"k8s.io/client-go/rest"

	"github.com/Mirantis/virtlet/pkg/utils"
)

// GetCRDRestClient returns ReST client that can be used to work with virtlet CRDs
func GetCRDRestClient(cfg *rest.Config) (*rest.RESTClient, error) {
	return utils.GetK8sRestClient(cfg, scheme, &SchemeGroupVersion)
}
