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
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/golang/glog"
)

const (
	minAcceptErrorDelay = 5 * time.Millisecond
	maxAcceptErrorDelay = 1 * time.Second
	receiveFdTimeout    = 5 * time.Second
	fdMagic             = 0x42424242
	fdAdd               = 0
	fdRelease           = 1
	fdGet               = 2
	fdResponse          = 0x80
	fdAddResponse       = fdAdd | fdResponse
	fdReleaseResponse   = fdRelease | fdResponse
	fdGetResponse       = fdGet | fdResponse
	fdError             = 0xff
)

// FDManager denotes an object that provides 'master'-side
// functionality of FDClient
type FDManager interface {
	AddFDs(key string, data interface{}) ([]byte, error)
	ReleaseFDs(key string) error
}

type fdHeader struct {
	Magic    uint32
	Command  uint8
	DataSize uint32
	OobSize  uint32
	Key      [64]byte
}

func (hdr *fdHeader) getKey() string {
	return strings.TrimSpace(string(hdr.Key[:]))
}

func fdKey(key string) [64]byte {
	var r [64]byte
	for n := range r {
		if n < len(key) {
			r[n] = key[n]
		} else {
			r[n] = 32
		}
	}
	return r
}

// FDSource denotes an 'executive' part for FDServer which
// creates and destroys (closes) the file descriptors and
// associated resources
type FDSource interface {
	// GetFDs sets up a file descriptors based on key
	// and extra data. It should return the file descriptor list,
	// any data that should be passed back to the client
	// invoking AddFDs() and an error, if any.
	GetFDs(key string, data []byte) ([]int, []byte, error)
	// Release destroys (closes) the file descriptor and
	// any associated resources
	Release(key string) error
	// GetInfo returns the information which needs to be
	// propagated back the FDClient upon GetFDs() call
	GetInfo(key string) ([]byte, error)
}

// FDServer listens on a Unix domain socket, serving requests to
// create, destroy and obtain file descriptors. It serves the purpose
// of sending the file descriptors across mount namespace boundaries,
// as well as making it easier to work around the Go namespace problem
// (to be fixed in Go 1.10):
// https://www.weave.works/blog/linux-namespaces-and-go-don-t-mix When
// the Go namespace problem is resolved, it should be possible to dumb
// down FDServer by making it only serve GetFDs() requests, performing
// other actions within the process boundary.
type FDServer struct {
	sync.Mutex
	lst        *net.UnixListener
	socketPath string
	source     FDSource
	fds        map[string][]int
	stopCh     chan struct{}
}

// NewFDServer returns an FDServer for the specified socket path and
// an FDSource
func NewFDServer(socketPath string, source FDSource) *FDServer {
	return &FDServer{
		socketPath: socketPath,
		source:     source,
		fds:        make(map[string][]int),
	}
}

func (s *FDServer) addFDs(key string, fds []int) bool {
	s.Lock()
	defer s.Unlock()
	if _, found := s.fds[key]; found {
		return false
	}
	s.fds[key] = fds
	return true
}

func (s *FDServer) removeFDs(key string) {
	s.Lock()
	defer s.Unlock()
	delete(s.fds, key)
}

func (s *FDServer) getFDs(key string) ([]int, error) {
	s.Lock()
	defer s.Unlock()
	fds, found := s.fds[key]
	if !found {
		return nil, fmt.Errorf("bad fd key: %q", key)
	}
	return fds, nil
}

// Serve makes FDServer listen on its socket in a new goroutine.
// It returns immediately. Use Stop() to stop listening.
func (s *FDServer) Serve() error {
	s.Lock()
	defer s.Unlock()
	if s.stopCh != nil {
		return errors.New("already listening")
	}
	addr, err := net.ResolveUnixAddr("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to resolve unix addr %q: %v", s.socketPath, err)
	}
	l, err := net.ListenUnix("unix", addr)
	if err != nil {
		l.Close()
		return fmt.Errorf("failed to listen on socket %q: %v", s.socketPath, err)
	}
	// Accept error handling is inspired by server.go in grpc
	s.stopCh = make(chan struct{})
	var delay time.Duration
	go func() {
		for {
			conn, err := l.AcceptUnix()
			if err != nil {
				if temp, ok := err.(interface {
					Temporary() bool
				}); ok && temp.Temporary() {
					glog.Warningf("Accept error: %v", err)
					if delay == 0 {
						delay = minAcceptErrorDelay
					} else {
						delay *= 2
					}
					if delay > maxAcceptErrorDelay {
						delay = maxAcceptErrorDelay
					}
					select {
					case <-time.After(delay):
						continue
					case <-s.stopCh:
						return
					}
				}
				select {
				case <-s.stopCh:
					// this error is expected
					return
				default:
				}
				glog.Errorf("Accept failed: %v", err)
				break
			}
			go func() {
				err := s.serveConn(conn)
				if err != nil {
					glog.Error(err)
				}
			}()
		}
	}()
	return nil
}

func (s *FDServer) serveAdd(c *net.UnixConn, hdr *fdHeader) (*fdHeader, []byte, error) {
	data := make([]byte, hdr.DataSize)
	if len(data) > 0 {
		if _, err := io.ReadFull(c, data); err != nil {
			return nil, nil, fmt.Errorf("error reading payload: %v", err)
		}
	}
	key := hdr.getKey()
	fds, respData, err := s.source.GetFDs(key, data)
	if err != nil {
		return nil, nil, fmt.Errorf("error getting fd: %v", err)
	}
	if !s.addFDs(key, fds) {
		return nil, nil, fmt.Errorf("fd key already exists: %q", err)
	}
	return &fdHeader{
		Magic:    fdMagic,
		Command:  fdAddResponse,
		DataSize: uint32(len(respData)),
		Key:      hdr.Key,
	}, respData, nil
}

func (s *FDServer) serveRelease(hdr *fdHeader) (*fdHeader, error) {
	key := hdr.getKey()
	if err := s.source.Release(key); err != nil {
		return nil, fmt.Errorf("error releasing fd: %v", err)
	}
	s.removeFDs(key)
	return &fdHeader{
		Magic:   fdMagic,
		Command: fdReleaseResponse,
		Key:     hdr.Key,
	}, nil
}

func (s *FDServer) serveGet(c *net.UnixConn, hdr *fdHeader) (*fdHeader, []byte, []byte, error) {
	key := hdr.getKey()
	fds, err := s.getFDs(key)
	if err != nil {
		return nil, nil, nil, err
	}
	info, err := s.source.GetInfo(key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("can't get key info: %v", err)
	}

	rights := syscall.UnixRights(fds...)
	return &fdHeader{
		Magic:    fdMagic,
		Command:  fdGetResponse,
		DataSize: uint32(len(info)),
		OobSize:  uint32(len(rights)),
		Key:      hdr.Key,
	}, info, rights, nil
}

func (s *FDServer) serveConn(c *net.UnixConn) error {
	defer c.Close()
	for {
		var hdr fdHeader
		if err := binary.Read(c, binary.BigEndian, &hdr); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("error reading the header: %v", err)
		}
		if hdr.Magic != fdMagic {
			return errors.New("bad magic")
		}

		var err error
		var respHdr *fdHeader
		var data, oobData []byte
		switch hdr.Command {
		case fdAdd:
			respHdr, data, err = s.serveAdd(c, &hdr)
		case fdRelease:
			respHdr, err = s.serveRelease(&hdr)
		case fdGet:
			respHdr, data, oobData, err = s.serveGet(c, &hdr)
		default:
			err = errors.New("bad command")
		}

		if err != nil {
			data = []byte(err.Error())
			oobData = nil
			respHdr = &fdHeader{
				Magic:    fdMagic,
				Command:  fdError,
				DataSize: uint32(len(data)),
				OobSize:  0,
			}
		}

		if err := binary.Write(c, binary.BigEndian, respHdr); err != nil {
			return fmt.Errorf("error writing response header: %v", err)
		}
		if len(data) > 0 || len(oobData) > 0 {
			if data == nil {
				data = []byte{}
			}
			if oobData == nil {
				oobData = []byte{}
			}
			if _, _, err = c.WriteMsgUnix(data, oobData, nil); err != nil {
				return fmt.Errorf("error writing payload: %v", err)
			}
		}
	}
	return nil
}

// Stop makes FDServer stop listening and close its socket
func (s *FDServer) Stop() {
	s.Lock()
	defer s.Unlock()
	if s.stopCh != nil {
		close(s.stopCh)
		s.lst.Close()
		s.stopCh = nil
	}
}

// FDClient can be used to connect to an FDServer listening on a Unix
// domain socket
type FDClient struct {
	socketPath string
	conn       *net.UnixConn
}

var _ FDManager = &FDClient{}

// NewFDClient returns an FDClient for specified socket path
func NewFDClient(socketPath string) *FDClient {
	return &FDClient{socketPath: socketPath}
}

// Connect makes FDClient connect to its socket. You must call
// Connect() method to be able to use the FDClient
func (c *FDClient) Connect() error {
	if c.conn != nil {
		return nil
	}

	addr, err := net.ResolveUnixAddr("unix", c.socketPath)
	if err != nil {
		return fmt.Errorf("failed to resolve unix addr %q: %v", c.socketPath, err)
	}

	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		return fmt.Errorf("can't connect to %q: %v", c.socketPath, err)
	}
	c.conn = conn
	return nil
}

// Close closes the connection to FDServer
func (c *FDClient) Close() error {
	var err error
	if c.conn != nil {
		err = c.conn.Close()
		c.conn = nil
	}
	return err
}

func (c *FDClient) request(hdr *fdHeader, data []byte) (*fdHeader, []byte, []byte, error) {
	hdr.Magic = fdMagic
	if c.conn == nil {
		return nil, nil, nil, errors.New("not connected")
	}

	if err := binary.Write(c.conn, binary.BigEndian, hdr); err != nil {
		return nil, nil, nil, fmt.Errorf("error writing request header: %v", err)
	}

	if len(data) > 0 {
		if err := binary.Write(c.conn, binary.BigEndian, data); err != nil {
			return nil, nil, nil, fmt.Errorf("error writing request payload: %v", err)
		}
	}

	var respHdr fdHeader
	if err := binary.Read(c.conn, binary.BigEndian, &respHdr); err != nil {
		return nil, nil, nil, fmt.Errorf("error reading response header: %v", err)
	}
	if respHdr.Magic != fdMagic {
		return nil, nil, nil, errors.New("bad magic")
	}

	respData := make([]byte, respHdr.DataSize)
	oobData := make([]byte, respHdr.OobSize)
	if len(respData) > 0 || len(oobData) > 0 {
		n, oobn, _, _, err := c.conn.ReadMsgUnix(respData, oobData)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("error reading the message: %v", err)
		}
		// ReadMsgUnix will read & discard a single byte if len(respData) == 0
		if n != len(respData) && (len(respData) != 0 || n != 1) {
			return nil, nil, nil, fmt.Errorf("bad data size: %d instead of %d", n, len(respData))
		}
		if oobn != len(oobData) {
			return nil, nil, nil, fmt.Errorf("bad oob data size: %d instead of %d", oobn, len(oobData))
		}
	}

	if respHdr.Command == fdError {
		return nil, nil, nil, fmt.Errorf("server returned error: %s", respData)
	}

	if respHdr.Command != hdr.Command|fdResponse {
		return nil, nil, nil, fmt.Errorf("unexpected command %02x", respHdr.Command)
	}

	return &respHdr, respData, oobData, nil
}

// AddFDs requests the FDServer to add a new file descriptor
// using its FDSource. It returns the info which is returned
// by FDSource's GetFDs() call
func (c *FDClient) AddFDs(key string, data interface{}) ([]byte, error) {
	bs, ok := data.([]byte)
	if !ok {
		var err error
		bs, err = json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("error marshalling json: %v", err)
		}
	}
	respHdr, respData, _, err := c.request(&fdHeader{
		Command:  fdAdd,
		DataSize: uint32(len(bs)),
		Key:      fdKey(key),
	}, bs)
	if err != nil {
		return nil, err
	}
	if respHdr.getKey() != key {
		return nil, fmt.Errorf("fd key mismatch in the server response")
	}
	return respData, nil
}

// ReleaseFDs makes FDServer to close the file descriptor and destroy
// any associated resources
func (c *FDClient) ReleaseFDs(key string) error {
	_, _, _, err := c.request(&fdHeader{
		Command: fdRelease,
		Key:     fdKey(key),
	}, nil)
	return err
}

// GetFDs requests file descriptors from the FDServer. It returns a
// list of file descriptors which is valid for current process and any
// associated data that was returned from FDSource's GetInfo() call
func (c *FDClient) GetFDs(key string) ([]int, []byte, error) {
	_, respData, oobData, err := c.request(&fdHeader{
		Command: fdGet,
		Key:     fdKey(key),
	}, nil)
	if err != nil {
		return nil, nil, err
	}

	scms, err := syscall.ParseSocketControlMessage(oobData)
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't parse socket control message: %v", err)
	}
	if len(scms) != 1 {
		return nil, nil, fmt.Errorf("unexpected number of socket control messages: %d instead of 1", len(scms))
	}

	fds, err := syscall.ParseUnixRights(&scms[0])
	if err != nil {
		return nil, nil, fmt.Errorf("can't decode file descriptors: %v", err)
	}
	return fds, respData, nil
}
