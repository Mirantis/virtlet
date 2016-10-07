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

package utils

import (
	"os"
	"io/ioutil"
	"regexp"
)

func GetvCPUsNum() (int, error) {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return 0, err
	}

	file, err := ioutil.ReadAll(f)
	if err != nil {
		return 0, err
	}
	re := regexp.MustCompile("processor( |\t)*: ")
	cpus := re.FindAllString(string(file), -1)

	return len(cpus), nil
}
