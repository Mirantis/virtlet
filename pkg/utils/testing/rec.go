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

package testing

import "strings"

type Record struct {
	Name string      `json:"name"`
	Data interface{} `json:"data,omitempty"`
}

type Recorder interface {
	Rec(name string, data interface{})
}

type nullRecorder struct{}

func (r *nullRecorder) Rec(name string, data interface{}) {}

var NullRecorder = &nullRecorder{}

type TopLevelRecorder struct {
	recs    []*Record
	filters []string
}

func NewToplevelRecorder() *TopLevelRecorder {
	return &TopLevelRecorder{}
}

func (r *TopLevelRecorder) Rec(name string, data interface{}) {
	if r.nameMatches(name) {
		r.recs = append(r.recs, &Record{Name: name, Data: data})
	}
}

func (r *TopLevelRecorder) Content() []*Record {
	return r.recs
}

func (r *TopLevelRecorder) Child(prefix string) *ChildRecorder {
	return NewChildRecorder(r, prefix)
}

func (r *TopLevelRecorder) AddFilter(filter string) {
	r.filters = append(r.filters, filter)
}

func (r *TopLevelRecorder) nameMatches(name string) bool {
	if len(r.filters) == 0 {
		return true
	}
	for _, f := range r.filters {
		if strings.Contains(name, f) {
			return true
		}
	}
	return false
}

type ChildRecorder struct {
	parent Recorder
	prefix string
}

func NewChildRecorder(parent Recorder, prefix string) *ChildRecorder {
	return &ChildRecorder{parent: parent, prefix: prefix}
}

func (r *ChildRecorder) Rec(name string, data interface{}) {
	r.parent.Rec(r.prefix+": "+name, data)
}

func (r *ChildRecorder) Child(prefix string) *ChildRecorder {
	return NewChildRecorder(r, prefix)
}
