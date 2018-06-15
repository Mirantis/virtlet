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

package gm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/ghodss/yaml"
	"github.com/golang/glog"
)

const (
	jsonDataIndent = "  "
)

// Verifier describes a type that can verify its contents
// against a Golden Master data file and also generate
// the contents of such file
type Verifier interface {
	// Suffix returns the suffix for the file name of the Golden Master
	// data file for this value.
	Suffix() string
	// Marshal generates the contents of the Golden Master data file.
	Marshal() ([]byte, error)
	// Verify returns true if the contents can be considered
	// the same as the value of the Verifier. It should not return
	// an error if content is invalid.
	Verify(content []byte) (bool, error)
}

type textVerifier string

var _ Verifier = textVerifier("")

func (v textVerifier) Suffix() string {
	return ".txt"
}

func (v textVerifier) Verify(content []byte) (bool, error) {
	return string(v) == string(content), nil
}

func (v textVerifier) Marshal() ([]byte, error) {
	return []byte(v), nil
}

type JSONVerifier struct {
	data interface{}
}

var _ Verifier = JSONVerifier{}

func NewJSONVerifier(data interface{}) JSONVerifier {
	return JSONVerifier{data}
}

func (v JSONVerifier) Suffix() string {
	return ".json"
}

func (v JSONVerifier) Verify(content []byte) (bool, error) {
	var curData interface{}
	if err := json.Unmarshal(content, &curData); err != nil {
		glog.Warningf("Failed to unmarshal to JSON: %v:\n%s", err, content)
		return false, nil
	}

	out, err := json.Marshal(v.data)
	if err != nil {
		return false, fmt.Errorf("failed to marshal data: %v. Input:\n%s",
			err, spew.Sdump(v))
	}

	var newData interface{}
	if err := json.Unmarshal(out, &newData); err != nil {
		return false, fmt.Errorf("failed to unmarshal back marshalled value: %v. JSON:\n%s\nOriginal data:\n%s",
			err, string(out), spew.Sdump(v))
	}

	return reflect.DeepEqual(curData, newData), nil
}

func (v JSONVerifier) Marshal() ([]byte, error) {
	switch d := v.data.(type) {
	case []byte:
		return d, nil
	case string:
		return []byte(d), nil
	default:
		out, err := json.MarshalIndent(v.data, "", jsonDataIndent)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal json data: %v. Input:\n%s",
				err, spew.Sdump(v.data))
		}
		return out, nil
	}
}

// YamlVerifier verifies the data using YAML representation.
type YamlVerifier struct {
	data interface{}
}

var _ Verifier = YamlVerifier{}

// NewYamlVerifier makes a YamlVerifier with the specified content.
func NewYamlVerifier(data interface{}) YamlVerifier {
	return YamlVerifier{data}
}

// Suffix implements Suffix method of the Verifier interface.
func (v YamlVerifier) Suffix() string {
	return ".yaml"
}

func marshalMultiple(data []interface{}) ([]byte, error) {
	var out bytes.Buffer
	for _, v := range data {
		bs, err := yaml.Marshal(v)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&out, "---\n%s", bs)
	}
	return out.Bytes(), nil
}

func unmarshalMultiple(in []byte) ([]interface{}, error) {
	var r []interface{}
	for _, part := range bytes.Split(in, []byte("---\n")) {
		if len(bytes.TrimSpace(part)) == 0 {
			continue
		}
		var data interface{}
		if err := yaml.Unmarshal(part, &data); err != nil {
			return nil, err
		}
		r = append(r, data)
	}
	return r, nil
}

func (v YamlVerifier) verifyMultiple(content []byte) (bool, error) {
	curData, err := unmarshalMultiple(content)
	if err != nil {
		glog.Warningf("Failed to unmarshal to YAML: %v:\n%s", err, content)
		return false, nil
	}

	out, err := v.Marshal()
	if err != nil {
		return false, err
	}

	newData, err := unmarshalMultiple(out)
	if err != nil {
		return false, fmt.Errorf("failed to unmarshal back marshalled value: %v. YAML:\n%s\nOriginal data:\n%s",
			err, string(out), content)
	}

	return reflect.DeepEqual(curData, newData), nil
}

// Verify implements Verify method of the Verifier interface.
func (v YamlVerifier) Verify(content []byte) (bool, error) {
	switch v.data.(type) {
	case []byte:
		return v.verifyMultiple(content)
	case string:
		return v.verifyMultiple(content)
	}

	var curData interface{}
	if err := yaml.Unmarshal(content, &curData); err != nil {
		glog.Warningf("Failed to unmarshal to YAML: %v:\n%s", err, content)
		return false, nil
	}

	out, err := v.Marshal()
	if err != nil {
		return false, err
	}

	var newData interface{}
	if err := yaml.Unmarshal(out, &newData); err != nil {
		return false, fmt.Errorf("failed to unmarshal back marshalled value: %v. YAML:\n%s\nOriginal data:\n%s",
			err, string(out), spew.Sdump(v))
	}

	return reflect.DeepEqual(curData, newData), nil
}

// Marshal implements Marshal method of the Verifier interface.
func (v YamlVerifier) Marshal() ([]byte, error) {
	switch d := v.data.(type) {
	case []byte:
		return d, nil
	case string:
		return []byte(d), nil
	default:
		out, err := yaml.Marshal(v.data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal yaml data: %v. Input:\n%s",
				err, spew.Sdump(v.data))
		}
		return out, nil
	}
}

// Replacement specifies a replacement for SubstVerifier.
type Replacement struct {
	Old string
	New string
}

// SubstVerifier wraps another verifier and replaces the specified
// substrings in the data it generates.
type SubstVerifier struct {
	next         Verifier
	replacements []Replacement
}

var _ Verifier = SubstVerifier{}

// NewSubstVerifier makes a SubstVerifier that wraps another verifier
// and does the specified replacements.
func NewSubstVerifier(next Verifier, replacements []Replacement) SubstVerifier {
	return SubstVerifier{next, replacements}
}

// Suffix implements Suffix method of the Verifier interface.
func (v SubstVerifier) Suffix() string {
	return v.next.Suffix()
}

// Verify implements Verify method of the Verifier interface.
func (v SubstVerifier) Verify(content []byte) (bool, error) {
	return v.next.Verify(content)
}

// Marshal implements Marshal method of the Verifier interface.
func (v SubstVerifier) Marshal() ([]byte, error) {
	d, err := v.next.Marshal()
	if err != nil {
		return nil, err
	}
	for _, rep := range v.replacements {
		d = bytes.Replace(d, []byte(rep.Old), []byte(rep.New), -1)
	}
	return d, nil
}

func getVerifier(data interface{}) Verifier {
	switch v := data.(type) {
	case Verifier:
		return v
	case string:
		return textVerifier(v)
	case []byte:
		return textVerifier(string(v))
	default:
		return NewJSONVerifier(v)
	}
}

var badFilenameCharRx = regexp.MustCompile(`[^\w-]`)

// GetFilenameForTest converts a Go test name to filename
func GetFilenameForTest(testName string, v interface{}) (string, error) {
	suffix := ".out" + getVerifier(v).Suffix()
	filename := strings.Replace(testName, "/", "__", -1)
	filename = badFilenameCharRx.ReplaceAllString(filename, "_") + suffix
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("can't get current directory: %v", err)
	}
	return filepath.Join(wd, filename), nil
}

// WriteDataFile serializes the specified value into a data file
func WriteDataFile(filename string, v interface{}) error {
	out, err := getVerifier(v).Marshal()
	if err != nil {
		return fmt.Errorf("error generating the data for %q: %v", filename, err)
	}
	if err := ioutil.WriteFile(filename, out, 0777); err != nil {
		return fmt.Errorf("error writing %q: %v", filename, err)
	}
	return nil
}

// DataFileDiffers compares the specified value against the stored data file
func DataFileDiffers(filename string, v interface{}) (bool, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("error reading %q: %v", filename, err)
	}

	ok, err := getVerifier(v).Verify(content)
	if err != nil {
		return false, fmt.Errorf("error parsing %q: %v", filename, err)
	}

	return !ok, nil
}
