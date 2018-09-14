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
	"fmt"
	"os/exec"
	"strings"
)

// Command represents an external command being prepared or run.
type Command interface {
	// Output runs the command and returns its standard output.
	// If stdin is non-nil, it's passed to the command's standed
	// input. Any returned error will usually be of type
	// *ExitError with ExitError.Stderr containing the stderr
	// output of the command.
	Run(stdin []byte) ([]byte, error)
}

// Commander is used to run external commands.
type Commander interface {
	// Command returns an isntance of Command to execute the named
	// program with the given arguments.
	Command(name string, arg ...string) Command
}

type realCommand struct {
	*exec.Cmd
}

func (c realCommand) Run(stdin []byte) ([]byte, error) {
	if stdin != nil {
		c.Stdin = bytes.NewBuffer(stdin)
	}
	out, err := c.Cmd.Output()
	if err == nil {
		return out, nil
	}
	fullCmd := strings.Join(append([]string{c.Path}, c.Args...), " ")
	if exitErr, ok := err.(*exec.ExitError); ok {
		return out, fmt.Errorf("command %s: %v: %s", fullCmd, exitErr, exitErr.Stderr)
	}
	return out, fmt.Errorf("command %s: %v", fullCmd, err)
}

type defaultCommander struct{}

func (c *defaultCommander) Command(name string, arg ...string) Command {
	return realCommand{exec.Command(name, arg...)}
}

// DefaultCommander is an implementation of Commander that runs the
// commands using exec.Command.
var DefaultCommander Commander = &defaultCommander{}
