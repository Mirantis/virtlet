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

package image

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

func extractImageSizeFromInfo(out []byte) (uint64, error) {
	var parsed struct {
		VirtualSize uint64 `json:"virtual-size"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return 0, fmt.Errorf("can't parse image info: %v\ninfo:\n%s", err, out)
	}
	return parsed.VirtualSize, nil
}

// GetImageVirtualSize returns the virtual size of the specified QCOW2 image
func GetImageVirtualSize(imagePath string) (uint64, error) {
	cmd := exec.Command("qemu-img", "info", "--output", "json", imagePath)
	out, err := cmd.Output()
	if err == nil {
		return extractImageSizeFromInfo(out)
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return 0, fmt.Errorf("qemu-img failed: %v\noutput:\n%s", err, exitErr.Stderr)
	}
	return 0, fmt.Errorf("qemu-img failed: %v", err)
}
