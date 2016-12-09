/*
Copyright 2016 Mirantis

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

package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"syscall"

	"google.golang.org/grpc"

	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	virtletSocket = "/tmp/virtlet.sock"
)

func createEnviron() []string {
	environ := os.Environ()

	environ = append(environ, "LIBGUESTFS_DEBUG=1")
	environ = append(environ, "LIBGUESTFS_TRACE=1")

	return environ
}

type VirtletManager struct {
	libvirtUri string
	pid        int
	conn       *grpc.ClientConn
	boltDbPath string
}

func NewVirtletManager() *VirtletManager {
	return &VirtletManager{
		libvirtUri: "qemu+tcp://localhost/system",
	}
}

func NewFakeVirtletManager() *VirtletManager {
	return &VirtletManager{
		libvirtUri: "test:///default",
	}
}

func (v *VirtletManager) Run() error {
	filename, err := utils.Tempfile()
	if err != nil {
		return err
	}

	boltPathParam := fmt.Sprintf("-bolt-path=%s", filename)
	libvirtUriParam := fmt.Sprintf("-libvirt-uri=%s", v.libvirtUri)
	listenParam := fmt.Sprintf("-listen=%s", virtletSocket)

	virtletPath, err := exec.LookPath("virtlet")
	if err != nil {
		return err
	}
	virtletDir := path.Dir(virtletPath)

	v.boltDbPath = filename

	pid, err := syscall.ForkExec(virtletPath, []string{
		virtletPath,
		boltPathParam,
		libvirtUriParam,
		listenParam,
		"-v=3",
		"-logtostderr=true",
	}, &syscall.ProcAttr{
		Dir:   virtletDir,
		Env:   createEnviron(),
		Files: []uintptr{0, 1, 2},
		Sys:   &syscall.SysProcAttr{},
	})
	if err != nil {
		return err
	}

	if err := waitForSocket(virtletSocket); err != nil {
		return err
	}

	conn, err := grpc.Dial(virtletSocket, grpc.WithInsecure(), grpc.WithDialer(utils.Dial))
	if err != nil {
		return err
	}

	v.pid = pid
	v.conn = conn

	return nil
}

func (v *VirtletManager) Close() {
	v.conn.Close()
	syscall.Kill(v.pid, syscall.SIGKILL)
	os.Remove(virtletSocket)
}
