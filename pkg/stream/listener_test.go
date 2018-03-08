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

package stream

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

func getServer() *UnixServer {
	baseDir, _ := ioutil.TempDir("", "virtlet-log")
	os.Mkdir(baseDir, 0777)
	socketPath := filepath.Join(baseDir, "streamer.sock")
	return NewUnixServer(socketPath, baseDir)
}

func TestAddOutputReader(t *testing.T) {
	u := getServer()
	ch1 := make(chan []byte, 1)
	ch2 := make(chan []byte, 1)
	u.AddOutputReader("1", ch1)
	u.AddOutputReader("1", ch2)
	outputReaders, ok := u.outputReaders["1"]
	if ok == false {
		t.Fatalf("No registered readers for podUID=1")
	}
	if len(outputReaders) != 2 {
		t.Errorf("Wrong numbers of readers. Expected 2, got: %d", len(outputReaders))
	}
}

func TestBroadcastMessageToManyReceivers(t *testing.T) {
	u := getServer()
	ch1 := make(chan []byte, 1)
	ch2 := make(chan []byte, 1)
	u.AddOutputReader("1", ch1)
	u.AddOutputReader("1", ch2)

	msg := []byte("test")
	u.broadcast("1", msg)

	select {
	case m1 := <-ch1:
		if string(m1) != string(msg) {
			t.Errorf("channel received wrong message. Expected: %s, got: %s", msg, m1)
		}
	default:
		t.Errorf("channel did not receive message")
	}
	select {
	case m1 := <-ch2:
		if string(m1) != string(msg) {
			t.Errorf("channel received wrong message. Expected: %s, got: %s", msg, m1)
		}
	default:
		t.Errorf("channel did not receive message")
	}
}

func TestStopListen(t *testing.T) {
	// setup server
	u := getServer()
	go u.Listen()
	err := waitForSocket(u.SocketPath, 5)
	if err != nil {
		t.Errorf("Error when waiting for socket to be created: %s", u.SocketPath)
	}

	// stop listening
	close(u.closeCh)

	passed := 0
loop:
	for {
		select {
		case <-u.listenDone:
			break loop
		default:
			if passed > 5 {
				t.Fatal("Listener didn't finsh in 5 seconds")
			}
			time.Sleep(time.Duration(1) * time.Second)
			passed++
		}
	}

	if len(u.outputReaders) > 0 {
		t.Errorf("Error after closing listener. outputReaders should be empty but contains: %v", u.outputReaders)
	}
}

func TestCleaningReader(t *testing.T) {
	// setup server
	u := getServer()
	go u.Listen()
	err := waitForSocket(u.SocketPath, 5)
	if err != nil {
		t.Errorf("Error when waiting for socket to be created: %s", u.SocketPath)
	}

	// setup client
	containerID := "1123ab2-baed-32e7-6d1d-13110da12345"
	tc := testutils.RunProcess(t, "nc", []string{"-U", u.SocketPath}, []string{
		"VIRTLET_POD_UID=8c8e8cf1-acea-11e7-8e0e-02420ac00002",
		"VIRTLET_CONTAINER_NAME=ubuntu",
		fmt.Sprintf("VIRTLET_CONTAINER_ID=%s", containerID),
		"CONTAINER_ATTEMPTS=0",
	})
	defer tc.Stop()

	u.Stop()
	outputReaders, ok := u.outputReaders[containerID]
	if ok == true {
		t.Errorf("Error after closing connection. outputReaders should be deleted but exists and contains: %v", outputReaders)
	}
}

func waitForSocket(path string, timeout int) error {
	passed := 0
	for {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			break
		}
		time.Sleep(time.Duration(1) * time.Second)
		passed++
		if passed >= timeout {
			return fmt.Errorf("Timeout")
		}
	}
	return nil
}
