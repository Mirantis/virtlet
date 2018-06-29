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

package main

import (
	"flag"
	"os"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	"github.com/Mirantis/virtlet/tests/longevity"
	"github.com/golang/glog"
)

func main() {
	baseTest := flag.Bool("base", false, "Run base longevity tests")
	stressTest := flag.Bool("stress", false, "Run longevity stress tests")

	flag.Parse()

	glog.Infof("Starting Virtlet longevity tests...")
	controller, err := framework.NewController("")
	if err != nil {
		glog.Fatal(err)
		os.Exit(1)
	}

	instances := []*longevity.VMInstance{}

	if *baseTest {
		instances = append(instances, longevity.GetBaseTests(controller)...)
	}
	if *stressTest {
		instances = append(instances, longevity.GetStressTests(controller)...)
	}

	err = longevity.Run(controller, instances)
	if err != nil {
		glog.Fatal(err)
		os.Exit(1)
	}
	controller.Finalize()
}
