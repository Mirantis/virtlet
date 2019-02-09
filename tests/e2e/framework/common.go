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

package framework

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	NginxImage   = "docker.io/nginx:1.14.2"
	BusyboxImage = "docker.io/busybox:1.30.0"
)

// ErrTimeout is the timeout error returned from functions wrapped by WithTimeout
var ErrTimeout = fmt.Errorf("timeout")

// CommandError holds an exit code for commands finished without any Executor error
type CommandError struct {
	ExitCode int
}

func (e CommandError) Error() string {
	return fmt.Sprintf("command finished with %d exit code", e.ExitCode)
}

var _ error = CommandError{}

// Command is the interface to control the command started with an Executor
type Command interface {
	Kill() error
	Wait() error
}

// Executor is the interface to run shell commands in arbitrary places
type Executor interface {
	io.Closer
	Run(stdin io.Reader, stdout, stderr io.Writer, command ...string) error
	Start(stdin io.Reader, stdout, stderr io.Writer, command ...string) (Command, error)
}

// Run executes command with the given executor, returns stdout/stderr as strings
// and exit code in CommandError
func Run(executor Executor, input string, command ...string) (string, string, error) {
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	inBuf := bytes.NewBufferString(input)
	err := executor.Run(inBuf, outBuf, errBuf, command...)
	return outBuf.String(), errBuf.String(), err
}

// RunSimple is a simplified version of Run that verifies exit code/stderr internally and returns stdout only
func RunSimple(executor Executor, command ...string) (string, error) {
	stdout, stderr, err := Run(executor, "", command...)
	if err != nil {
		if ce, ok := err.(CommandError); ok {
			if ce.ExitCode != 0 {
				return "", fmt.Errorf("command exited with code %d, stderr: %s", ce.ExitCode, strings.TrimSpace(stderr)+strings.TrimSpace(stdout))
			}
			return strings.TrimSpace(stdout), nil
		}
		return "", err
	}
	return strings.TrimSpace(stdout), nil
}

func trimBlock(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.Join(lines, "\n")
}

func waitFor(f func() error, wait, poll time.Duration, waitFailure bool) error {
	if poll <= 0 || wait <= 0 {
		wait = time.Duration(time.Hour)
		poll = 0
	}
	timeout := time.After(wait)
	err := f()
	if err == nil && !waitFailure || err != nil && waitFailure {
		return err
	}
	result := err
	for {
		select {
		case <-time.After(poll):
			err := f()
			if err == nil && !waitFailure || err != nil && waitFailure {
				return err
			}
			result = err
			if poll == 0 {
				return result
			}
		case <-timeout:
			return result
		}
	}
}

func waitForConsistentState(f func() error, timing ...time.Duration) error {
	if len(timing) == 0 {
		panic("timing is not provided")
	}
	var pollPeriod time.Duration
	if len(timing) == 1 || timing[1] <= 0 {
		pollPeriod = time.Duration(timing[0].Nanoseconds() / 10)
	} else {
		pollPeriod = timing[1]
	}
	var err error
	for timing[0] > 0 {
		now := time.Now()
		if err = waitFor(f, timing[0], pollPeriod, false); err != nil {
			timing[0] -= time.Now().Sub(now)
			continue
		}

		if len(timing) >= 2 {
			now := time.Now()
			if err = waitFor(f, timing[2], pollPeriod, true); err != nil {
				timing[0] -= time.Now().Sub(now)
				continue
			}
		}
		break
	}
	return err
}

// WithTimeout adds timeout to synchronous function
func WithTimeout(timeout time.Duration, fn func() error) func() error {
	return func() error {
		res := make(chan error, 1)
		go func() {
			res <- fn()
		}()
		timer := time.After(timeout)
		select {
		case e := <-res:
			return e
		case <-timer:
			return ErrTimeout
		}
	}
}
