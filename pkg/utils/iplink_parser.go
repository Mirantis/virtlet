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

package utils

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
)

// VFInfo contains information about particular VF
type VFInfo struct {
	// ID contains VF number
	ID int
	// Mac contains hardware mac address
	Mac string
	// VLanID contains vlan id
	VLanID int
	// SpoofChecking holds status of spoof checking
	SpoofChecking bool
	// LinkState holds state of link (nil means auto)
	LinkState *bool
}

// helper func to create pointer to boolean value
func newBool(value bool) *bool {
	tmp := value
	return &tmp
}

// ParseIPLinkOutput takes output of `ip link show somelink` and parses
// it to list of VFInfo structures
func ParseIPLinkOutput(data []byte) ([]VFInfo, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineRx := regexp.MustCompile(
		`vf (\d+) MAC ([^,]+), vlan (\d+), spoof checking (on|off), link-state (auto|enable|disable)`)

	var vfinfos []VFInfo
	for scanner.Scan() {
		s := scanner.Text()
		parts := lineRx.FindStringSubmatch(s)
		if parts == nil {
			continue
		}

		id, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as int: %v", parts[1], err)
		}
		vlanid, err := strconv.Atoi(parts[3])
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as int: %v", parts[3], err)
		}
		spfc := false
		if parts[4] == "on" {
			spfc = true
		}
		var state *bool
		switch parts[5] {
		case "enable":
			state = newBool(true)
		case "disable":
			state = newBool(false)
		}
		vfinfos = append(vfinfos, VFInfo{
			ID:            id,
			Mac:           parts[2],
			VLanID:        vlanid,
			SpoofChecking: spfc,
			LinkState:     state,
		})
	}

	return vfinfos, nil
}
