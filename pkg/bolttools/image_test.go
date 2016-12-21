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

package bolttools

import "testing"

func TestGetImageName(t *testing.T) {
	tests := []struct {
		volumeName, imageName string
	}{
		{
			volumeName: "my-favorite-distro",
			imageName:  "example.com/my-favorite-distro",
		},
		{
			volumeName: "another-distro",
			imageName:  "another.example.com/another-distro",
		},
	}

	for _, tc := range tests {
		b, err := NewFakeBoltClient()
		if err != nil {
			t.Fatal(err)
		}

		if err := b.EnsureImageSchema(); err != nil {
			t.Fatal(err)
		}

		err = b.SetImageName(tc.volumeName, tc.imageName)
		if err != nil {
			t.Fatal(err)
		}

		imageName, err := b.GetImageName(tc.volumeName)
		if err != nil {
			t.Fatal(err)
		}

		if imageName != tc.imageName {
			t.Errorf("Bad imageName: expected %q, got %q instead", tc.imageName, imageName)
		}
	}
}

func TestGetNonExistentImageName(t *testing.T) {
	b, err := NewFakeBoltClient()
	if err != nil {
		t.Fatal(err)
	}

	if err := b.EnsureImageSchema(); err != nil {
		t.Fatal(err)
	}

	imageName, err := b.GetImageName("no-such-volume")
	if err != nil {
		t.Fatal(err)
	}

	if imageName != "" {
		t.Errorf("Bad imageName for non-existent image: %q instead of an empty string", imageName)
	}
}

func TestRemoveImage(t *testing.T) {
	b, err := NewFakeBoltClient()
	if err != nil {
		t.Fatal(err)
	}

	if err = b.EnsureImageSchema(); err != nil {
		t.Fatal(err)
	}

	if err = b.SetImageName("another-distro", "another.example.com/another-distro"); err != nil {
		t.Fatal(err)
	}

	if err = b.RemoveImage("another-distro"); err != nil {
		t.Fatal(err)
	}

	imageName, err := b.GetImageName("another-distro")
	if err != nil {
		t.Fatal(err)
	}

	if imageName != "" {
		t.Errorf("Bad imageName for removed image: %q instead of an empty string", imageName)
	}
}
