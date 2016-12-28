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

// TODO: credits
// (based on pkg/kubelet/api/testing/utils.go from k8s)
package testing

import (
	"fmt"
	"reflect"
	"sync"

	runtimeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

func BuildContainerName(metadata *runtimeapi.ContainerMetadata, sandboxID string) string {
	// include the sandbox ID to make the container ID unique.
	return fmt.Sprintf("%s_%s_%d", sandboxID, metadata.GetName(), metadata.GetAttempt())
}

func BuildSandboxName(metadata *runtimeapi.PodSandboxMetadata) string {
	return fmt.Sprintf("%s_%s_%s_%d", metadata.GetName(), metadata.GetNamespace(), metadata.GetUid(), metadata.GetAttempt())
}

func filterInLabels(filter, labels map[string]string) bool {
	for k, v := range filter {
		if value, ok := labels[k]; ok {
			if value != v {
				return false
			}
		} else {
			return false
		}
	}

	return true
}

type Journal interface {
	Record(item string)
}

type SimpleJournal struct {
	sync.Mutex
	Items []string
}

func NewSimpleJournal() *SimpleJournal { return &SimpleJournal{} }

func (j *SimpleJournal) Record(item string) {
	j.Lock()
	defer j.Unlock()

	j.Items = append(j.Items, item)
}

func (j *SimpleJournal) Verify(expectedItems []string) error {
	j.Lock()
	defer j.Unlock()

	actualItems := j.Items
	j.Items = nil
	if !reflect.DeepEqual(actualItems, expectedItems) {
		return fmt.Errorf("bad journal items. Expected %v, got %v", expectedItems, actualItems)
	}
	return nil
}

type PrefixJournal struct {
	journal Journal
	prefix  string
}

func NewPrefixJournal(journal Journal, prefix string) *PrefixJournal {
	return &PrefixJournal{journal, prefix}
}

func (j *PrefixJournal) Record(item string) {
	j.journal.Record(j.prefix + item)
}
