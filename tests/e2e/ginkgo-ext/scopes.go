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
	"reflect"
	"sync/atomic"

	"github.com/onsi/ginkgo"
)

type scope struct {
	parent  *scope
	count   int32
	before  func()
	after   func()
	started int32
	failed  bool
}

var (
	currentScope = &scope{}

	Context                      = wrapContextFunc(ginkgo.Context)
	FContext                     = wrapContextFunc(ginkgo.FContext)
	PContext                     = wrapContextFunc(ginkgo.PContext)
	XContext                     = wrapContextFunc(ginkgo.XContext)
	Describe                     = wrapContextFunc(ginkgo.Describe)
	FDescribe                    = wrapContextFunc(ginkgo.FDescribe)
	PDescribe                    = wrapContextFunc(ginkgo.PDescribe)
	XDescribe                    = wrapContextFunc(ginkgo.XDescribe)
	It                           = wrapItFunc(ginkgo.It)
	FIt                          = wrapItFunc(ginkgo.FIt)
	PIt                          = wrapItFunc(muteParameters(ginkgo.PIt))
	XIt                          = wrapItFunc(muteParameters(ginkgo.XIt))
	By                           = ginkgo.By
	JustBeforeEach               = ginkgo.JustBeforeEach
	BeforeSuite                  = ginkgo.BeforeSuite
	AfterSuite                   = ginkgo.AfterSuite
	Skip                         = ginkgo.Skip
	Fail                         = ginkgo.Fail
	CurrentGinkgoTestDescription = ginkgo.CurrentGinkgoTestDescription
	GinkgoRecover                = ginkgo.GinkgoRecover
	GinkgoT                      = ginkgo.GinkgoT
)

type Done ginkgo.Done

func BeforeAll(body func()) bool {
	currentScope.before = body
	return BeforeEach(func() {})
}

func AfterAll(body func()) bool {
	currentScope.after = body
	return AfterEach(func() {})
}

func BeforeEach(body interface{}, timeout ...float64) bool {
	cs := currentScope
	before := func() {
		if atomic.CompareAndSwapInt32(&cs.started, 0, 1) && cs.before != nil {
			defer func() {
				if r := recover(); r != nil {
					cs.failed = true
					panic(r)
				}
			}()
			cs.before()
		} else if cs.failed {
			Fail("failed due to BeforeAll failure")
		}
	}
	return ginkgo.BeforeEach(applyAdvice(body, before, nil), timeout...)
}

func AfterEach(body interface{}, timeout ...float64) bool {
	cs := currentScope
	after := func() {
		if cs.count == 0 && cs.after != nil {
			cs.after()
		}
	}
	return ginkgo.AfterEach(applyAdvice(body, nil, after), timeout...)
}

func wrapContextFunc(fn func(string, func()) bool) func(string, func()) bool {
	return func(text string, body func()) bool {
		newScope := &scope{parent: currentScope}
		currentScope = newScope
		res := fn(text, body)
		currentScope = currentScope.parent
		return res
	}
}

func wrapItFunc(fn func(string, interface{}, ...float64) bool) func(string, interface{}, ...float64) bool {
	return func(text string, body interface{}, timeout ...float64) bool {
		s := currentScope
		for s != nil {
			s.count++
			s = s.parent
		}
		return fn(text, wrapTest(body), timeout...)
	}
}

func muteParameters(fn func(string, ...interface{}) bool) func(string, interface{}, ...float64) bool {
	return func(text string, _ interface{}, _ ...float64) bool {
		return fn(text)
	}
}

func applyAdvice(f interface{}, before, after func()) interface{} {
	fn := reflect.ValueOf(f)
	template := func(in []reflect.Value) []reflect.Value {
		if before != nil {
			before()
		}
		res := fn.Call(in)
		if after != nil {
			after()
		}
		return res
	}
	v := reflect.MakeFunc(fn.Type(), template)
	return v.Interface()
}

func wrapTest(f interface{}) interface{} {
	cs := currentScope
	after := func() {
		for cs != nil {
			atomic.AddInt32(&cs.count, -1)
			cs = cs.parent
		}
	}
	return applyAdvice(f, nil, after)
}
