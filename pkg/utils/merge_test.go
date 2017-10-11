/*
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

// This is mostly a copy of https://github.com/divideandconquer/go-merge/blob/master/merge/merge_test.go
// with minimal number of changes to reflect the different approach taken to slice merging (+ different package name)
// Original test function names are retained

package utils

import (
	"reflect"
	"testing"
)

func TestMerge(t *testing.T) {
	for _, tc := range []struct {
		name string
		a    interface{}
		b    interface{}
		r    interface{}
	}{
		{
			name: "non-overlapping maps",
			a:    map[string]string{"a": "X"},
			b:    map[string]string{"b": "Y"},
			r:    map[string]string{"a": "X", "b": "Y"},
		},
		{
			name: "overlapping maps",
			a:    map[string]string{"a": "X", "b": "Y"},
			b:    map[string]string{"c": "Z", "b": "Q"},
			r:    map[string]string{"a": "X", "c": "Z", "b": "Q"},
		},
		{
			name: "slices",
			a:    []int{1, 2},
			b:    []int{2, 3},
			r:    []int{1, 2, 2, 3},
		},
		{
			name: "slice-maps",
			a:    map[string][]int{"a": {1, 2}},
			b:    map[string][]int{"a": {3, 4}},
			r:    map[string][]int{"a": {1, 2, 3, 4}},
		},
		{
			name: "scalars",
			a:    3,
			b:    0,
			r:    0,
		},
		{
			name: "incompatible types",
			a:    []string{"a", "b"},
			b:    []int{1, 2},
			r:    []int{1, 2},
		},
		{
			name: "nil-slice",
			a:    []int{1, 2},
			b:    []int(nil),
			r:    []int{1, 2},
		},
		{
			name: "double nil-slice",
			a:    []int(nil),
			b:    []int(nil),
			r:    []int(nil),
		},
		{
			name: "nils",
			a:    nil,
			b:    nil,
			r:    nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Merge(tc.a, tc.b)
			if !reflect.DeepEqual(r, tc.r) {
				t.Errorf("expected: %v, actual: %v", tc.r, r)
			}
		})
	}
}
