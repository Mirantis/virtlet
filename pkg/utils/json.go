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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
)

// ToJSON converts the specified object to indented JSON.
// It panics in case if the object connot be converted.
func ToJSON(o interface{}) string {
	bs, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		log.Panicf("error marshalling json: %v", err)
	}
	return string(bs)
}

// ToJSONUnindented converts the specified object to unindented JSON.
// It panics in case if the object connot be converted.
func ToJSONUnindented(o interface{}) string {
	bs, err := json.Marshal(o)
	if err != nil {
		log.Panicf("error marshalling json: %v", err)
	}
	return string(bs)
}

// ReadJSON converts data from file specified by `filename` to provided as `v`
// interface.
func ReadJSON(filename string, v interface{}) error {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("error reading json file %q: %v", filename, err)
	}

	if err := json.Unmarshal(content, v); err != nil {
		return fmt.Errorf("failed to parse json file %q: %v", filename, err)
	}

	return nil
}

// WriteJSON saves under specified `filename` data provided in `v` interface
// setting file mode according to `perm` value.
func WriteJSON(filename string, v interface{}, perm os.FileMode) error {
	content, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("couldn't marshal the data to JSON for %q: %v", filename, err)
	}
	if err := ioutil.WriteFile(filename, content, perm); err != nil {
		return fmt.Errorf("error writing JSON data file %q: %v", filename, err)
	}
	return nil
}
