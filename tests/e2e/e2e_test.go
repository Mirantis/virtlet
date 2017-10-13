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

package e2e

import (
	"flag"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var controller *framework.Controller
var cirrosLocation = flag.String("cirros", defaultCirrosLocation, "cirros image URL (*without http(s)://*")

func TestE2E(t *testing.T) {
	SetDefaultEventuallyTimeout(time.Minute * 5)
	RegisterFailHandler(Fail)

	BeforeAll(func() {
		var err error
		controller, err = framework.NewController("")
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("Using namespace %s", controller.Namespace()))
	})
	AfterAll(func() {
		controller.Finalize()
	})

	RunSpecs(t, "Virtlet E2E suite")
}
