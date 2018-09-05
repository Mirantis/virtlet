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

import "testing"

func TestNewShellTemplate(t *testing.T) {
	tmpl := NewShellTemplate("mount {{ shq .Foobar }} {{ .Baz }}")
	out, err := tmpl.ExecuteToString(map[string]string{
		"Foobar": "abc def",
		"Baz":    "qqq",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	expectedText := "mount 'abc def' qqq"
	if out != expectedText {
		t.Errorf("Bad template result. Expected:\n%s\nGot:\n%s", expectedText, out)
	}
}
