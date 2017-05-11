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
package criproxy

import (
	"context"
	"net"
	"os"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
)

const (
	connectAttemptInterval = 500 * time.Millisecond
)

// getContextWithTimeout returns a context with timeout.
func getContextWithTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

// dial creates a net.Conn by unix socket addr.
func dial(addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", addr, timeout)
}

func waitForSocket(path string, maxAttempts int, extraCheck func() error) error {
	var err error
	var conn net.Conn
	for n := 0; maxAttempts < 0 || n < maxAttempts; n++ {
		if _, err = os.Stat(path); err != nil {
			glog.V(1).Infof("attempt %d: %q is not here yet: %v", n, path, err)
		} else if conn, err = dial(path, connectWaitTimeout); err != nil {
			glog.V(1).Infof("attempt %d: can't connect to %q yet: %v", n, path, err)
		} else {
			conn.Close()
			if extraCheck != nil {
				err = extraCheck()
				if err != nil {
					glog.V(1).Infof("attempt %d: extra check failed for %q: %v", n, path, err)
					continue
				}
			}
			break
		}
		time.Sleep(connectAttemptInterval)
	}
	return err
}

// TODO: remove this
func init() {
	// Make spew output more readable for k8s runtime API objects
	spew.Config.DisableMethods = true
	spew.Config.DisablePointerMethods = true
}
