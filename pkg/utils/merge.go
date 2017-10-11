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

/*
The code to recursively merge JSON-like data structures (maps, slices, values)
Based on go-merge library (https://github.com/divideandconquer/go-merge) with some major changes
*/

package utils

import (
	"reflect"
)

// Merge will take two data sets and merge them together - returning a new data set
func Merge(base, override interface{}) interface{} {
	//reflect and recurse
	b := reflect.ValueOf(base)
	o := reflect.ValueOf(override)
	ret := mergeRecursive(b, o)
	if !ret.IsValid() {
		return nil
	}

	return ret.Interface()
}

func mergeRecursive(base, override reflect.Value) reflect.Value {
	if !override.IsValid() || !base.IsValid() || base.Type() != override.Type() {
		return override
	}
	var result reflect.Value

	switch base.Kind() {
	case reflect.Ptr:
		switch base.Elem().Kind() {
		case reflect.Ptr:
			fallthrough
		case reflect.Slice:
			fallthrough
		case reflect.Map:
			// Pointers to complex types should recurse if they aren't nil
			if base.IsNil() {
				result = override
			} else if override.IsNil() {
				result = base
			} else {
				result = mergeRecursive(base.Elem(), override.Elem())

			}
		default:
			// Pointers to basic types should just override
			result = override
		}

	case reflect.Interface:
		// Interfaces should just be unwrapped and recursed through
		result = mergeRecursive(base.Elem(), override.Elem())

	case reflect.Map:

		// For Maps we copy the base data, and then replace it with merged data
		// We use two for loops to make sure all map keys from base and all keys from
		// override exist in the result just in case one of the maps is sparse.
		elementsAreValues := base.Type().Elem().Kind() != reflect.Ptr

		result = reflect.MakeMap(base.Type())
		// Copy from base first
		for _, key := range base.MapKeys() {
			result.SetMapIndex(key, base.MapIndex(key))
		}

		// Override with values from override if they exist
		if override.Kind() == reflect.Map {
			for _, key := range override.MapKeys() {
				overrideVal := override.MapIndex(key)
				baseVal := base.MapIndex(key)
				if !overrideVal.IsValid() {
					continue
				}

				// if there is no base value, just set the override
				if !baseVal.IsValid() {
					result.SetMapIndex(key, overrideVal)
					continue
				}

				// Merge the values and set in the result
				newVal := mergeRecursive(baseVal, overrideVal)
				if elementsAreValues && newVal.Kind() == reflect.Ptr {
					result.SetMapIndex(key, newVal.Elem())

				} else {
					result.SetMapIndex(key, newVal)
				}
			}
		}
	case reflect.Slice:
		result = reflect.AppendSlice(base, override)
	default:
		result = override
	}
	return result
}
