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

package utils

import (
	"flag"
	"os"
	"strings"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// GetK8sClientConfig returns config that is needed to access k8s
func GetK8sClientConfig(host string) (*rest.Config, error) {
	if host == "" {
		url, exists := os.LookupEnv("KUBERNETES_CLUSTER_URL")
		if !exists {
			return rest.InClusterConfig()
		}
		host = url
	}
	return &rest.Config{Host: host}, nil
}

// GetK8sClientset returns clientset for standard k8s APIs
func GetK8sClientset(config *rest.Config) (*kubernetes.Clientset, error) {
	if config == nil {
		var err error
		config, err = GetK8sClientConfig("")
		if err != nil {
			return nil, err
		}
	}
	return kubernetes.NewForConfig(config)
}

// GetK8sRestClient returns k8s ReST client that for the giver API group-version/subset
func GetK8sRestClient(cfg *rest.Config, scheme *runtime.Scheme, groupVersion *schema.GroupVersion) (*rest.RESTClient, error) {
	config := *cfg
	config.GroupVersion = groupVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: serializer.NewCodecFactory(scheme)}

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// wordSepNormalizeFunc change "_" to "-" in the flags.
func wordSepNormalizeFunc(f *pflag.FlagSet, name string) pflag.NormalizedName {
	if strings.Contains(name, "_") {
		return pflag.NormalizedName(strings.Replace(name, "_", "-", -1))
	}
	return pflag.NormalizedName(name)
}

// defaultClientConfig builds a default Kubernetes client config based
// on Cobra flags. It's based on kubelet code.
func defaultClientConfig(flags *pflag.FlagSet) clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	// use the standard defaults for this client command
	// DEPRECATED: remove and replace with something more accurate
	loadingRules.DefaultClientConfig = &clientcmd.DefaultClientConfig

	flags.StringVar(&loadingRules.ExplicitPath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests.")

	overrides := &clientcmd.ConfigOverrides{ClusterDefaults: clientcmd.ClusterDefaults}

	flagNames := clientcmd.RecommendedConfigOverrideFlags("")
	// short flagnames are disabled by default.  These are here for compatibility with existing scripts
	flagNames.ClusterOverrideFlags.APIServer.ShortName = "s"

	clientcmd.BindOverrideFlags(overrides, flags, flagNames)
	clientConfig := clientcmd.NewInteractiveDeferredLoadingClientConfig(loadingRules, overrides, os.Stdin)

	return clientConfig
}

// BindFlags applies standard Go flags and the flags used by kubeclient
// to the specified FlagSet.
func BindFlags(flags *pflag.FlagSet) clientcmd.ClientConfig {
	flags.AddGoFlagSet(flag.CommandLine)
	flags.SetNormalizeFunc(wordSepNormalizeFunc)
	return defaultClientConfig(flags)
}
