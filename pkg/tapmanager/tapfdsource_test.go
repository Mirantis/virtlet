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

package tapmanager

import (
	"net"
	"testing"
)

// TODO: add test for TapFDSource itself

func verifyNetmaskForCalico(t *testing.T, a, b net.IP, expectedOnes int) {
	mask, err := calcNetmaskForCalico(a, b)
	if err != nil {
		t.Fatalf("%v - %v: can't calculate netmask: %v", err)
	}
	ones, bits := mask.Size()
	if bits != 32 {
		t.Errorf("%v - %v: bad mask bit count %d (expected 32)", a, b, bits)
	}
	if ones != expectedOnes {
		t.Errorf("%v - %v: bad mask ones count %d (expected %d)", a, b, ones, expectedOnes)
	}
}

func TestCalcNetmaskForCalico(t *testing.T) {
	for _, tc := range []struct {
		a, b net.IP
		ones int
	}{
		// byte 3: 10000101, 10000110
		{net.IP{192, 168, 135, 133}, net.IP{192, 168, 135, 134}, 30},
		// byte 3: 10000100, 10000101
		{net.IP{192, 168, 135, 132}, net.IP{192, 168, 135, 133}, 29},
		// byte 2: 10101001, 10000111
		{net.IP{192, 168, 169, 129}, net.IP{192, 168, 135, 133}, 18},
	} {
		verifyNetmaskForCalico(t, tc.a, tc.b, tc.ones)
		verifyNetmaskForCalico(t, tc.b, tc.a, tc.ones)
	}
}
