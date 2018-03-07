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
	"flag"
	"os"

	"github.com/spf13/pflag"
	"k8s.io/client-go/tools/clientcmd"
	"strings"
)

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

// from k8s pkg/kubectl/plugins/env.go
func flagToEnvName(flagName, prefix string) string {
	envName := strings.TrimPrefix(flagName, "--")
	envName = strings.ToUpper(envName)
	envName = strings.Replace(envName, "-", "_", -1)
	envName = prefix + envName
	return envName
}

// BindFlags applies go flags to the specified FlagSet and sets global
// flag values from kubectl plugin environment variables in case if
// virtletctl is running as a kubectl plugin.
func BindFlags(flags *pflag.FlagSet) clientcmd.ClientConfig {
	flags.AddGoFlagSet(flag.CommandLine)
	flags.SetNormalizeFunc(wordSepNormalizeFunc)
	clientConfig := defaultClientConfig(flags)
	if InPlugin() {
		for _, flagName := range []string{
			// k8s client flags
			"kubeconfig",
			clientcmd.FlagClusterName,
			clientcmd.FlagAuthInfoName,
			clientcmd.FlagContext,
			clientcmd.FlagNamespace,
			clientcmd.FlagAPIServer,
			clientcmd.FlagInsecure,
			clientcmd.FlagCertFile,
			clientcmd.FlagKeyFile,
			clientcmd.FlagCAFile,
			clientcmd.FlagBearerToken,
			clientcmd.FlagImpersonate,
			clientcmd.FlagImpersonateGroup,
			clientcmd.FlagUsername,
			clientcmd.FlagPassword,
			clientcmd.FlagTimeout,

			// glog flags
			"alsologtostderr",
			"log-backtrace-at",
			"log-dir",
			"logtostderr",
			"stderrthreshold",
			"v",
			"vmodule",
		} {
			v, found := os.LookupEnv(flagToEnvName(flagName, "KUBECTL_PLUGINS_GLOBAL_FLAG_"))
			if found && (flagName != clientcmd.FlagImpersonateGroup || v != "[]") {
				flags.Set(flagName, v)
			}
		}
	}
	return clientConfig
}
