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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
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
