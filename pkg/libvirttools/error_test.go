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

package libvirttools

import (
	"fmt"
	"testing"
)

func TestConvert(t *testing.T) {
	tests := []struct {
		cError        int
		expectedError error
	}{
		{
			cError:        0,
			expectedError: nil,
		},

		// image.h
		{
			cError:        1001, // VIRTLET_IMAGE_ERR_SEND_STREAM
			expectedError: fmt.Errorf("Failed to send/save image stream (VIRTLET_IMAGE_ERR_SEND_STREAM)"),
		},
		// We are ignoring already existing images
		{
			cError:        1002, // VIRTLET_IMAGE_ERR_ALREADY_EXISTS
			expectedError: nil,
		},
		{
			cError:        1003, // VIRTLET_IMAGE_ERR_LIBVIRT
			expectedError: GetFakeLibvirtError(),
		},

		// virtualization.h
		{
			cError:        2001, // VIRTLET_VIRTUALIZATION_ERR_LIBVIRT
			expectedError: GetFakeLibvirtError(),
		},
	}

	fakeCErrorHandler := NewFakeCErrorHandler()

	for _, tc := range tests {
		err := fakeCErrorHandler.ConvertGoInt(tc.cError)

		if err == nil && tc.expectedError == nil {
			continue
		}

		if err != nil && tc.expectedError != nil && err.Error() == tc.expectedError.Error() {
			continue
		}

		t.Errorf("Expected error '%v', instead got '%v'", tc.expectedError, err)
	}
}
