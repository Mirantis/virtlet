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

package dhcp

import (
	"io/ioutil"

	"github.com/golang/protobuf/proto"
)

type Configuration struct {
	EndpointConfigurations []*EndpointConfiguration
	configPath             string
}

func NewConfiguration(path string) (*Configuration, error) {
	configs := &AllConfigurations{}

	if buffer, err := ioutil.ReadFile(path); err != nil {
		return nil, err
	} else if err = proto.Unmarshal(buffer, configs); err != nil {
		return nil, err
	}

	return &Configuration{
		EndpointConfigurations: configs.GetEndpointConfigurations(),
		configPath:             path,
	}, nil
}

func (c *Configuration) Save() error {
	allConfigurations := &AllConfigurations{
		EndpointConfigurations: c.EndpointConfigurations,
	}

	if data, err := proto.Marshal(allConfigurations); err != nil {
		return err
	} else if err := ioutil.WriteFile(c.configPath, data, 0600); err != nil {
		return err
	}

	return nil
}
