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

import "testing"

func TestGetBoolFromString(t *testing.T) {
	tests := []struct {
		str   string
		value bool
	}{
		{"", false},
		{"0", false},
		{"f", false},
		{"F", false},
		{"false", false},
		{"False", false},
		{"FALSE", false},
		{"FaLsE", false},
		// Anything else should be true
		{"Adsa", true},
		{"1", true},
		{"true", true},
		{"TrUe", true},
	}
	for _, test := range tests {
		v := GetBoolFromString(test.str)
		if v != test.value {
			t.Errorf("Error when converting string `%s` to bool. Expected %t, got %t", test.str, test.value, v)
		}
	}
}
