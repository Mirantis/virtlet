/*
Copyright 2016-2017 Mirantis

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

package manager

import (
	"testing"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

func TestCRIImages(t *testing.T) {
	tst := makeVirtletCRITester(t)
	defer tst.teardown()
	tst.listImages(nil)
	tst.pullImage(cirrosImg())
	tst.pullImage(ubuntuImg())
	tst.listImages(nil)
	tst.listImages(&kubeapi.ImageFilter{Image: cirrosImg()})
	tst.imageStatus(cirrosImg())
	tst.removeImage(cirrosImg())
	tst.imageStatus(cirrosImg())
	tst.listImages(nil)
	// second RemoveImage() should not cause an error
	tst.removeImage(cirrosImg())
	tst.verify()
}
