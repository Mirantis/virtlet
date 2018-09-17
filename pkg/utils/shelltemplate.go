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
	"bytes"
	"log"
	"text/template"

	"github.com/kballard/go-shellquote"
)

// ShellTemplate denotes a simple template used to generate shell
// commands. It adds 'shellquote' function to go-template that can be
// used to quote string values for the shell.
type ShellTemplate struct {
	*template.Template
}

// NewShellTemplate returns a new shell template using the specified
// text. It panics if the template doesn't compile.
func NewShellTemplate(text string) *ShellTemplate {
	funcs := map[string]interface{}{
		"shq": func(text string) string {
			return shellquote.Join(text)
		},
	}
	return &ShellTemplate{
		template.Must(template.New("script").Funcs(funcs).Parse(text)),
	}
}

// ExecuteToString executes the template using the specified data and
// returns the result as a string.
func (t *ShellTemplate) ExecuteToString(data interface{}) (string, error) {
	var buf bytes.Buffer
	err := t.Template.Execute(&buf, data)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// MustExecuteToString executes the template using the specified data
// and returns the result as a string. It panics upon an error
func (t *ShellTemplate) MustExecuteToString(data interface{}) string {
	r, err := t.ExecuteToString(data)
	if err != nil {
		log.Panicf("Error executing template: %v", err)
	}
	return r
}
