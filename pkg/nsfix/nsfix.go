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

package nsfix

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"syscall"

	"github.com/golang/glog"
)

// ReexecHandler is a function that can be passed to
// RegisterReexec to be executed my nsfix mechanism after
// self-reexec. arg can be safely casted to the type of arg
// passed to RegisterReexec plus one level of pointer
// inderection, i.e. if you pass somestruct{} to RegisterReexec
// you may cast arg safely to *somestruct.
type ReexecHandler func(arg interface{}) (interface{}, error)

type handlerEntry struct {
	handler ReexecHandler
	argType reflect.Type
}

var reexecMap = map[string]handlerEntry{}

type retStruct struct {
	Success bool
	Result  json.RawMessage
	Error   string
}

// RegisterReexec registers the specified function as a reexec handler.
// arg specifies the argument type to pass. Note that if you pass somestruct{}
// as arg, the handler will receive *somestruct as its argument (i.e. a level
// of pointer indirection is added).
func RegisterReexec(name string, handler ReexecHandler, arg interface{}) {
	reexecMap[name] = handlerEntry{handler, reflect.TypeOf(arg)}
}

func getGlogLevel() int {
	// XXX: apparently there's no better way to get the log level
	i := 0
	for ; i < 100; i++ {
		if !glog.V(glog.Level(i)) {
			return i - 1
		}
	}
	return i
}

func restoreGlogLevel() {
	logLevelStr := os.Getenv("NSFIX_LOG_LEVEL")
	if logLevelStr == "" {
		logLevelStr = "1"
	}
	// configure glog (apparently no better way to do it ...)
	flag.CommandLine.Parse([]string{"-v=" + logLevelStr, "-logtostderr=true"})
}

func marshalResult(ret interface{}, retErr error) ([]byte, error) {
	var r retStruct
	if retErr != nil {
		r.Error = retErr.Error()
	} else {
		resultBytes, err := json.Marshal(ret)
		if err != nil {
			return nil, fmt.Errorf("error marshalling the result: %v", err)
		}

		r.Success = true
		r.Result = json.RawMessage(resultBytes)
	}
	retBytes, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("error marshalling retStruct: %v", err)
	}
	return retBytes, nil
}

func unmarshalResult(retBytes []byte, ret interface{}) error {
	var r retStruct
	if err := json.Unmarshal(retBytes, &r); err != nil {
		return fmt.Errorf("error unmarshalling the result: %v", err)
	}
	if !r.Success {
		return errors.New(r.Error)
	}
	if ret != nil {
		if err := json.Unmarshal(r.Result, ret); err != nil {
			return fmt.Errorf("error unmarshalling the result: %v", err)
		}
	}
	return nil
}

// HandleReexec handles executing the code in another namespace.
// If reexcution is requested, the function calls os.Exit() after
// handling it.
func HandleReexec() {
	if os.Getenv("NSFIX_NS_PID") == "" {
		return
	}

	restoreGlogLevel()
	handlerName := os.Getenv("NSFIX_HANDLER")
	if handlerName == "" {
		glog.Fatal("NSFIX_HANDLER not set")
	}
	entry, found := reexecMap[handlerName]
	if !found {
		glog.Fatalf("Bad NSFIX_HANDLER %q", handlerName)
	}

	var arg interface{}
	if entry.argType != nil {
		arg = reflect.New(entry.argType).Interface()
		argStr := os.Getenv("NSFIX_ARG")
		if argStr != "" {
			if err := json.Unmarshal([]byte(argStr), arg); err != nil {
				glog.Fatalf("Can't unmarshal NSFIX_ARG (NSFIX_HANDLER %q):\n%s\n", handlerName, argStr)
			}
		}
	}

	spawned := os.Getenv("NSFIX_SPAWN") != ""
	switch ret, err := entry.handler(arg); {
	case err != nil && !spawned:
		glog.Fatalf("Error invoking NSFIX_HANDLER %q: %v", handlerName, err)
	case err == nil && !spawned:
		os.Exit(0)
	default:
		outBytes, err := marshalResult(ret, err)
		if err != nil {
			glog.Fatalf("Error marshalling the result from NSFIX_HANDLER %q: %v", handlerName, err)
		}
		os.Stdout.Write(outBytes)
		os.Exit(0)
	}
}

// Call describes a call to be executed in network, mount, UTS
// and IPC namespaces of another process.
type Call struct {
	targetPid   int
	handlerName string
	arg         interface{}
	remountSys  bool
	dropPrivs   bool
}

// NewCall makes a new Call structure with specified
// handlerName using PID 1.
func NewCall(handlerName string) *Call {
	return &Call{
		targetPid:   1,
		handlerName: handlerName,
	}
}

// TargetPid sets target PID value for Call
func (c *Call) TargetPid(targetPid int) *Call {
	c.targetPid = targetPid
	return c
}

// Arg sets argument for Call
func (c *Call) Arg(arg interface{}) *Call {
	c.arg = arg
	return c
}

// RemountSys instructs Call to remount /sys in the new process
func (c *Call) RemountSys() *Call {
	c.remountSys = true
	return c
}

// DropPrivs instructs Call to drop privileges in the new process
func (c *Call) DropPrivs() *Call {
	c.dropPrivs = true
	return c
}

func (c *Call) getEnvForExec(spawn bool) ([]string, error) {
	env := os.Environ()
	filteredEnv := []string{}
	for _, envItem := range env {
		if !strings.HasPrefix(envItem, "NSFIX_") {
			filteredEnv = append(filteredEnv, envItem)
		}
	}

	if c.arg != nil {
		argBytes, err := json.Marshal(c.arg)
		if err != nil {
			return nil, fmt.Errorf("error marshalling handler arg: %v", err)
		}
		filteredEnv = append(filteredEnv, fmt.Sprintf("NSFIX_ARG=%s", argBytes))
	}

	if c.remountSys {
		filteredEnv = append(filteredEnv, "NSFIX_REMOUNT_SYS=1")
	}

	if c.dropPrivs {
		filteredEnv = append(filteredEnv, "NSFIX_DROP_PRIVS=1")
	}

	if spawn {
		filteredEnv = append(filteredEnv, "NSFIX_SPAWN=1")
	}

	return append(filteredEnv,
		fmt.Sprintf("NSFIX_NS_PID=%d", c.targetPid),
		fmt.Sprintf("NSFIX_HANDLER=%s", c.handlerName),
		fmt.Sprintf("NSFIX_LOG_LEVEL=%d", getGlogLevel())), nil
}

// SwitchToNamespaces executes the specified handler using network,
// mount, UTS and IPC namespaces of the specified process. It passes
// the argument to the handler using JSON serialization. The current
// process gets replaced by the new one. If dropPrivs is true, the new
// process will execute using non-root uid/gid (using real uid/gid of
// the process if they're non-zero or 65534 which is nobody/nogroup)
func (c *Call) SwitchToNamespaces() error {
	env, err := c.getEnvForExec(false)
	if err != nil {
		return err
	}
	return syscall.Exec(os.Args[0], os.Args[:1], env)
}

// SpawnInNamespaces executes the specified handler using network,
// mount, UTS and IPC namespaces of the specified process. It passes
// the argument to the handler using JSON serialization. It then
// returns the value returned by the handler (also via JSON
// serialization + deserialization). If dropPrivs is true, the new
// process will execute using non-root uid/gid (using real uid/gid of
// the process if they're non-zero or 65534 which is nobody/nogroup)
func (c *Call) SpawnInNamespaces(ret interface{}) error {
	env, err := c.getEnvForExec(true)
	if err != nil {
		return err
	}

	cmd := exec.Command(os.Args[0])
	cmd.Env = env
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("reexec caused error: %v", err)
	}

	return unmarshalResult(out, ret)
}
