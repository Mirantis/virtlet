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
	"strconv"
	"strings"
)

// GetBoolFromString returns false if str is an empty string
// or is equal to one of: "0", "f" or "false". Case doesnt't matter
// anythging else is true
func GetBoolFromString(str string) bool {
	if str == "" {
		return false
	}
	str = strings.ToLower(str)
	b, err := strconv.ParseBool(str)
	if err != nil {
		b = true
	}
	return b
}
