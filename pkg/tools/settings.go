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
	"os"
	"strings"

	"github.com/spf13/pflag"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	defaultVirtletRuntimeName = "virtlet.cloud"
)

var (
	virtletRuntime = defaultVirtletRuntimeName
)

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
	clientConfig := utils.BindFlags(flags)
	flags.StringVar(&virtletRuntime, "virtlet-runtime", defaultVirtletRuntimeName, "the name of virtlet runtime used in kubernetes.io/target-runtime annotation")
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

			// virtletctl flags
			"virtlet-runtime",
		} {
			v, found := os.LookupEnv(flagToEnvName(flagName, "KUBECTL_PLUGINS_GLOBAL_FLAG_"))
			if found && (flagName != clientcmd.FlagImpersonateGroup || v != "[]") {
				flags.Set(flagName, v)
			}
		}
	}
	return clientConfig
}
