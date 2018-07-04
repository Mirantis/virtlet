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
	"reflect"
	"testing"
)

func TestIPLinkParser(t *testing.T) {
	tests := []struct {
		str     []byte
		vfinfos []VFInfo
	}{
		{
			[]byte("4: enp6s0: <BROADCAST,MULTICAST> mtu 1500 qdisc noop state DOWN mode DEFAULT group default qlen 1000\n" +
				"    link/ether e4:1d:2d:13:ed:30 brd ff:ff:ff:ff:ff:ff\n" +
				"    vf 0 MAC 00:00:00:00:00:00, vlan 4095, spoof checking off, link-state auto\n" +
				"    vf 1 MAC aa:bb:cc:dd:ee:ff, vlan 1, spoof checking on, link-state enable\n" +
				"    vf 2 MAC aa:bb:cc:dd:ee:fe, vlan 2, spoof checking on, link-state disable\n"),
			[]VFInfo{
				{ID: 0, Mac: "00:00:00:00:00:00", VLanID: 4095},
				{ID: 1, Mac: "aa:bb:cc:dd:ee:ff", VLanID: 1, SpoofChecking: true, LinkState: &[]bool{true}[0]},
				{ID: 2, Mac: "aa:bb:cc:dd:ee:fe", VLanID: 2, SpoofChecking: true, LinkState: &[]bool{false}[0]},
			},
		},
	}

	for _, test := range tests {
		vfinfos, err := ParseIPLinkOutput(test.str)
		if err != nil {
			t.Errorf("Unexpected error during parsing of test data: %v", err)
		}
		if len(vfinfos) != len(test.vfinfos) {
			t.Errorf("Expected %d infos about vfs while got %d", len(test.vfinfos), len(vfinfos))
		}
		if !reflect.DeepEqual(vfinfos, test.vfinfos) {
			t.Errorf("While expected %v got %v", test.vfinfos, vfinfos)
		}
	}
}
