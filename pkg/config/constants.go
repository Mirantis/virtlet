/*
Copyright 2016-2018 Mirantis

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

package config

const (
	// ContainerIDEnvVarName contains name of env variable passed from virtlet to vmwrapper
	ContainerIDEnvVarName = "VIRTLET_CONTAINER_ID"
	// CpusetsEnvVarName contains name of env variable passed from virtlet to vmwrapper
	CpusetsEnvVarName = "VIRTLET_CPUSETS"
	// EmulatorEnvVarName contains name of env variable passed from virtlet to vmwrapper
	EmulatorEnvVarName = "VIRTLET_EMULATOR"
	// LogPathEnvVarName contains name of env variable passed from virtlet to vmwrapper
	LogPathEnvVarName = "VIRTLET_CONTAINER_LOG_PATH"
	// NetKeyEnvVarName contains name of env variable passed from virtlet to vmwrapper
	NetKeyEnvVarName = "VIRTLET_NET_KEY"
)
