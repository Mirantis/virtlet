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

// Record denotes an item saved by Recorder's Rec method.
type Record struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value,omitempty"`
}

// Recorder is used to record various events for use in tests.
type Recorder interface {
	// Rec adds an item to Recorder using the specified name and data.
	Rec(name string, data interface{})
}

type nullRecorder struct{}

func (r *nullRecorder) Rec(name string, data interface{}) {}

// NullRecorder ignores all the items passed to it via Rec.
var NullRecorder = &nullRecorder{}

// TopLevelRecorder records the items as-is, optionally
// applying filters to them. It can also create child
// recorders and give back its current contents.
type TopLevelRecorder struct {
	recs    []*Record
	filters []string
}

// NewToplevelRecorder creates a new TopLevelRecorder.
func NewToplevelRecorder() *TopLevelRecorder {
	return &TopLevelRecorder{}
}

// Rec implements Rec method of Recorder interface.
func (r *TopLevelRecorder) Rec(name string, value interface{}) {
	if r.nameMatches(name) {
		r.recs = append(r.recs, &Record{Name: name, Value: value})
	}
}

// Content returns the current contents of the recorder.
func (r *TopLevelRecorder) Content() []*Record {
	return r.recs
}

// Child creates a Child recorder for this TopLevelRecorder that will
// add the specified prefix (plus ": ") to the names of items it
// records.
func (r *TopLevelRecorder) Child(prefix string) *ChildRecorder {
	return NewChildRecorder(r, prefix)
}

// AddFilter adds a new filter substring to the TopLevelRecorder.  If
// any filter substrings are specified, Rec will ignore items with
// names that don't include any of these substrings (i.e. the filters
// are ORed)
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

// ChildRecorder is a recorder that prefixes the names of its items
// with the specified prefix and then passes them to its parent
// recorder.
type ChildRecorder struct {
	parent Recorder
	prefix string
}

// NewChildRecorder creates a new ChildRecorder.
func NewChildRecorder(parent Recorder, prefix string) *ChildRecorder {
	return &ChildRecorder{parent: parent, prefix: prefix}
}

// Rec implements Rec method of Recorder interface.
func (r *ChildRecorder) Rec(name string, data interface{}) {
	r.parent.Rec(r.prefix+": "+name, data)
}

// Child creates a Child recorder for this recorder.
func (r *ChildRecorder) Child(prefix string) *ChildRecorder {
	return NewChildRecorder(r, prefix)
}
