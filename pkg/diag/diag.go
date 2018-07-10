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

// The backoff code for temporary Accept() errors is based on gRPC
// code. Original copyright notice follows:
/*
 *
 * Copyright 2014, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package diag

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/golang/glog"
)

const (
	toplevelDirName = "diagnostics"
)

// Result denotes the result of a diagnostics run.
type Result struct {
	// Name is the name of the item sans extension.
	Name string `json:"name,omitempty"`
	// Ext is the file extension to use.
	Ext string `json:"ext,omitempty"`
	// Data is the content returned by the Source.
	Data string `json:"data,omitempty"`
	// IsDir specifies whether this diagnostics result
	// needs to be unpacked to a directory.
	IsDir bool `json:"isdir"`
	// Children denotes the child items to be placed into the
	// subdirectory that should be made for this Result during
	// unpacking.
	Children map[string]Result `json:"children,omitempty"`
	// Error contains an error message in case if the Source
	// has failed to provide the information.
	Error string `json:"error,omitempty"`
}

// FileName returns the file name for this Result.
func (dr Result) FileName() string {
	if dr.Ext != "" {
		return fmt.Sprintf("%s.%s", dr.Name, dr.Ext)
	}
	return dr.Name
}

// Unpack unpacks Result under the specified directory.
func (dr Result) Unpack(parentDir string) error {
	switch {
	case dr.Name == "":
		return errors.New("Result name is not set")
	case dr.Error != "":
		glog.Warningf("Error recorded for the diag item %q: %v", dr.Name, dr.Error)
		return nil
	case !dr.IsDir && len(dr.Children) != 0:
		return errors.New("Result can't contain both Data and Children")
	case dr.IsDir:
		dirPath := filepath.Join(parentDir, dr.FileName())
		if err := os.MkdirAll(dirPath, 0777); err != nil {
			return err
		}
		for _, child := range dr.Children {
			if err := child.Unpack(dirPath); err != nil {
				return fmt.Errorf("couldn't unpack diag result at %q: %v", dirPath, err)
			}
		}
		return nil
	default:
		targetPath := filepath.Join(parentDir, dr.FileName())
		if err := ioutil.WriteFile(targetPath, []byte(dr.Data), 0777); err != nil {
			return fmt.Errorf("error writing %q: %v", targetPath, err)
		}
		return nil
	}
}

// ToJSON encodes Result into JSON.
func (dr Result) ToJSON() []byte {
	bs, err := json.Marshal(dr)
	if err != nil {
		log.Panicf("Error marshalling Result: %v", err)
	}
	return bs
}

// Source speicifies a diagnostics information source
type Source interface {
	// DiagnosticInfo returns diagnostic information for the
	// source. DiagnosticInfo() may skip setting Name in the
	// Result, in which case it'll be set to the name used to
	// register the source.
	DiagnosticInfo() (Result, error)
}

// Set denotes a set of diagnostics sources.
type Set struct {
	sync.Mutex
	sources map[string]Source
}

// NewDiagSet creates a new Set.
func NewDiagSet() *Set {
	return &Set{sources: make(map[string]Source)}
}

// RegisterDiagSource registers a diagnostics source.
func (ds *Set) RegisterDiagSource(name string, source Source) {
	ds.Lock()
	defer ds.Unlock()
	ds.sources[name] = source
}

// RunDiagnostics collects the diagnostic information from all of the
// available sources.
func (ds *Set) RunDiagnostics() Result {
	ds.Lock()
	defer ds.Unlock()
	r := Result{
		Name:     toplevelDirName,
		IsDir:    true,
		Children: make(map[string]Result),
	}
	for name, src := range ds.sources {
		dr, err := src.DiagnosticInfo()
		if dr.Name == "" {
			dr.Name = name
		}
		if err != nil {
			r.Children[name] = Result{
				Name:  dr.Name,
				Error: err.Error(),
			}
		} else {
			r.Children[name] = dr
		}
	}
	return r
}

// Server denotes a diagnostics server that listens on a unix domain
// socket and spews out a piece of JSON content on a socket
// connection.
type Server struct {
	sync.Mutex
	ds     *Set
	ln     net.Listener
	doneCh chan struct{}
}

// NewServer makes a new diagnostics server using the specified Set.
// If diagSet is nil, DefaultDiagSet is used.
func NewServer(diagSet *Set) *Server {
	if diagSet == nil {
		diagSet = DefaultDiagSet
	}
	return &Server{ds: diagSet}
}

func (s *Server) dump(conn net.Conn) error {
	defer conn.Close()
	r := s.ds.RunDiagnostics()
	bs, err := json.Marshal(&r)
	if err != nil {
		return fmt.Errorf("error marshalling diagnostics info: %v", err)
	}
	switch n, err := conn.Write(bs); {
	case err != nil:
		return err
	case n < len(bs):
		return errors.New("short write")
	}
	return nil
}

// Serve makes the server listen on the specified socket path. If
// readyCh is not nil, it'll be closed when the server is ready to
// accept connections. This function doesn't return till the server
// stops listening.
func (s *Server) Serve(socketPath string, readyCh chan struct{}) error {
	err := syscall.Unlink(socketPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	s.Lock()
	s.doneCh = make(chan struct{})
	defer close(s.doneCh)
	s.ln, err = net.Listen("unix", socketPath)
	s.Unlock()
	if err != nil {
		return err
	}
	defer s.ln.Close()
	if readyCh != nil {
		close(readyCh)
	}
	for {
		var tempDelay time.Duration // how long to sleep on accept failure

		for {
			conn, err := s.ln.Accept()
			if err != nil {
				if ne, ok := err.(interface {
					Temporary() bool
				}); !ok || !ne.Temporary() {
					glog.V(1).Infof("done serving; Accept = %v", err)
					return err
				}
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				glog.Warningf("Accept error: %v; retrying in %v", err, tempDelay)
				<-time.After(tempDelay)
				continue
			}
			tempDelay = 0

			if err := s.dump(conn); err != nil {
				glog.Warningf("Error dumping diagnostics info: %v", err)
			}
		}
	}
}

// Stop stops the server.
func (s *Server) Stop() {
	s.Lock()
	if s.ln != nil {
		s.ln.Close()
		s.Unlock()
		<-s.doneCh
		s.doneCh = nil
	} else {
		s.Unlock()
	}
}

// RetrieveDiagnostics retrieves the diagnostic info from the
// specified UNIX domain socket.
func RetrieveDiagnostics(socketPath string) (Result, error) {
	addr, err := net.ResolveUnixAddr("unix", socketPath)
	if err != nil {
		return Result{}, fmt.Errorf("failed to resolve unix addr %q: %v", socketPath, err)
	}

	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		return Result{}, fmt.Errorf("can't connect to %q: %v", socketPath, err)
	}

	bs, err := ioutil.ReadAll(conn)
	if err != nil {
		return Result{}, fmt.Errorf("can't read diagnostics: %v", err)
	}

	return DecodeDiagnostics(bs)
}

// DecodeDiagnostics loads the diagnostics info from the JSON data.
func DecodeDiagnostics(data []byte) (Result, error) {
	var r Result
	if err := json.Unmarshal(data, &r); err != nil {
		return Result{}, fmt.Errorf("error unmarshalling the diagnostics: %v", err)
	}
	return r, nil
}

// CommandSource executes the specified command and returns the stdout
// contents as diagnostics info
type CommandSource struct {
	ext string
	cmd []string
}

var _ Source = &CommandSource{}

// NewCommandSource creates a new CommandSource.
func NewCommandSource(ext string, cmd []string) *CommandSource {
	return &CommandSource{
		ext: ext,
		cmd: cmd,
	}
}

// DiagnosticInfo implements DiagnosticInfo method of the Source
// interface.
func (s *CommandSource) DiagnosticInfo() (Result, error) {
	if len(s.cmd) == 0 {
		return Result{}, errors.New("empty command")
	}
	r := Result{
		Ext: s.ext,
	}
	out, err := exec.Command(s.cmd[0], s.cmd[1:]...).Output()
	if err == nil {
		r.Data = string(out)
	} else {
		cmdStr := strings.Join(s.cmd, " ")
		if ee, ok := err.(*exec.ExitError); ok {
			return Result{}, fmt.Errorf("error running command %q: stderr:\n%s", cmdStr, ee.Stderr)
		}
		return Result{}, fmt.Errorf("error running command %q: %v", cmdStr, err)
	}
	return r, nil
}

// SimpleTextSourceFunc denotes a function that's invoked by
// SimpleTextSource to gather diagnostics info.
type SimpleTextSourceFunc func() (string, error)

// SimpleTextSource invokes the specified function that returns a
// string (and an error, if any) and wraps its result in Result
type SimpleTextSource struct {
	ext    string
	toCall SimpleTextSourceFunc
}

var _ Source = &SimpleTextSource{}

// NewSimpleTextSource creates a new SimpleTextSource.
func NewSimpleTextSource(ext string, toCall SimpleTextSourceFunc) *SimpleTextSource {
	return &SimpleTextSource{
		ext:    ext,
		toCall: toCall,
	}
}

// DiagnosticInfo implements DiagnosticInfo method of the Source
// interface.
func (s *SimpleTextSource) DiagnosticInfo() (Result, error) {
	out, err := s.toCall()
	if err != nil {
		return Result{}, err
	}
	return Result{
		Ext:  s.ext,
		Data: out,
	}, nil
}

// LogDirSource bundles together log files from the specified directory.
type LogDirSource struct {
	logDir string
}

// NewLogDirSource creates a new LogDirSource.
func NewLogDirSource(logDir string) *LogDirSource {
	return &LogDirSource{
		logDir: logDir,
	}
}

var _ Source = &LogDirSource{}

// DiagnosticInfo implements DiagnosticInfo method of the Source
// interface.
func (s *LogDirSource) DiagnosticInfo() (Result, error) {
	files, err := ioutil.ReadDir(s.logDir)
	if err != nil {
		return Result{}, err
	}
	r := Result{
		IsDir:    true,
		Children: make(map[string]Result),
	}
	for _, fi := range files {
		if fi.IsDir() {
			continue
		}
		name := fi.Name()
		ext := filepath.Ext(name)
		cur := Result{
			Name: name,
		}
		if ext != "" {
			cur.Ext = ext[1:]
			cur.Name = name[:len(name)-len(ext)]
		}
		fullPath := filepath.Join(s.logDir, name)
		data, err := ioutil.ReadFile(fullPath)
		if err != nil {
			return Result{}, fmt.Errorf("error reading %q: %v", fullPath, err)
		}
		cur.Data = string(data)
		r.Children[cur.Name] = cur
	}
	return r, nil
}

type stackDumpSource struct{}

func (s stackDumpSource) DiagnosticInfo() (Result, error) {
	var buf []byte
	var stackSize int
	bufSize := 32768
	for {
		buf = make([]byte, bufSize)
		stackSize = runtime.Stack(buf, true)
		if stackSize < len(buf) {
			break
		}
		bufSize *= 2
	}
	return Result{
		Ext:  "log",
		Data: string(buf[:stackSize]),
	}, nil
}

// StackDumpSource dumps Go runtime stack.
var StackDumpSource Source = stackDumpSource{}

// DefaultDiagSet is the default Set to use.
var DefaultDiagSet = NewDiagSet()

func init() {
	DefaultDiagSet.RegisterDiagSource("stack", StackDumpSource)
}
