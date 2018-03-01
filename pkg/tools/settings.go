/*
Copyright 2018 Mirantis

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

package tools

import (
	"fmt"
	"os/user"
	"path/filepath"

	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	DefaultKubeConfig = "~/.kube/config"
)

var (
	kubeConfig string
	apiServer  string
)

func AddGlobalFlags(fs *pflag.FlagSet) {
	fs.StringVar(&kubeConfig, "kubeconfig", DefaultKubeConfig, "absolute path to the kubeconfig file")
	fs.StringVar(&apiServer, "apiserver", "", "apiserver url")
}

func getKubeClient() (*rest.Config, kubernetes.Interface, error) {
	configPath := kubeConfig
	if kubeConfig[:2] == "~/" {
		usr, err := user.Current()
		if err != nil {
			return nil, nil, err
		}
		configPath = filepath.Join(usr.HomeDir, kubeConfig[2:])
	}

	config, err := clientcmd.BuildConfigFromFlags(apiServer, configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("Can't create kubernetes api client config: %v", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("Can't create kubernetes api client: %v", err)
	}

	return config, client, nil
}
