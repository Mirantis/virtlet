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

// ErrTimeout is the timeout error returned from functions wrapped by WithTimeout
var ErrTimeout = fmt.Errorf("timeout")

// Executor is the interface to execute shell commands in arbitrary places
type Executor interface {
	io.Closer
	Exec(command []string, stdin io.Reader, stdout, stderr io.Writer) (int, error)
}

// Exec executes command with the given executor and returns stdout/stderr/exitCode as strings
func Exec(executor Executor, command []string, input string) (string, string, int, error) {
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	inBuf := bytes.NewBufferString(input)
	exitCode, err := executor.Exec(command, inBuf, outBuf, errBuf)
	if err != nil {
		return "", "", 0, err
	}
	return outBuf.String(), errBuf.String(), exitCode, nil
}

// ExecSimple is a simplified version of Exec that verifies exit code/stderr internally and returns stdout only
func ExecSimple(executor Executor, command ...string) (string, error) {
	stdout, stderr, exitcode, err := Exec(executor, command, "")
	if err != nil {
		return "", err
	}
	if exitcode != 0 {
		return "", fmt.Errorf("command exited with code %d, stderr: %s", exitcode, strings.TrimSpace(stderr))
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
	if err := waitFor(f, timing[0], pollPeriod, false); err != nil {
		return err
	}

	if len(timing) >= 2 {
		if err := waitFor(f, timing[2], pollPeriod, true); err != nil {
			return err
		}
	}
	return nil
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
