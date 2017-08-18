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

package ginkgoext

import (
	"github.com/onsi/gomega/types"
)

type anythingMatcher struct{}

func (matcher *anythingMatcher) Match(actual interface{}) (success bool, err error) {
	return true, nil
}

func (matcher *anythingMatcher) FailureMessage(actual interface{}) (message string) {
	return ""
}

func (matcher *anythingMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return ""
}

// BeAnything returns matcher that matches any value
func BeAnything() types.GomegaMatcher {
	return &anythingMatcher{}
}
