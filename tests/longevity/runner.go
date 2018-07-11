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

package longevity

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	"github.com/golang/glog"
)

func GetBaseTests(controller *framework.Controller) []*VMInstance {
	return []*VMInstance{
		{
			name:         "cirros-base-test",
			controller:   controller,
			lifetime:     time.Duration(24) * time.Hour,
			testWaitTime: time.Duration(1) * time.Hour,
		},
	}
}

func GetStressTests(controller *framework.Controller) []*VMInstance {
	return []*VMInstance{
		{
			name:         "cirros-stress-1min",
			controller:   controller,
			lifetime:     time.Duration(1) * time.Minute,
			testWaitTime: time.Duration(1) * time.Minute,
		},
		{
			name:         "cirros-stress-3min",
			controller:   controller,
			lifetime:     time.Duration(3) * time.Minute,
			testWaitTime: time.Duration(1) * time.Minute,
		},
		{
			name:         "cirros-stress-5min",
			controller:   controller,
			lifetime:     time.Duration(5) * time.Minute,
			testWaitTime: time.Duration(1) * time.Minute,
		},
		{
			name:         "cirros-stress-10min",
			controller:   controller,
			lifetime:     time.Duration(10) * time.Minute,
			testWaitTime: time.Duration(1) * time.Minute,
		},
		{
			name:         "cirros-stress-15min",
			controller:   controller,
			lifetime:     time.Duration(15) * time.Minute,
			testWaitTime: time.Duration(1) * time.Minute,
		},
	}
}

func Run(controller *framework.Controller, instances []*VMInstance) error {
	var err error
	errChan := make(chan error)

	exitChan := make(chan os.Signal, 1)
	signal.Notify(exitChan, os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-exitChan:
			glog.V(4).Infof("CTRL-C received")
			cancel()
		case <-ctx.Done():
			break
		}
	}()

	_, err = startNginxPod(controller)
	if err != nil {
		return fmt.Errorf("Couldn't start  nginx pod: %v", err)
	}

	for _, instance := range instances {
		glog.Infof("Creating `%s` VM...", instance.name)
		err = instance.Create()
		if err != nil {
			return fmt.Errorf("Could not create VM: %v", err)
		}
		glog.V(4).Infof("Done")
	}
	for _, instance := range instances {
		go instance.Test(ctx, instance, testVM, errChan)
	}

	for {
		select {
		case err = <-errChan:
			glog.V(4).Infof("Received error: %v", err)
			cancel()
			return err
		case <-ctx.Done():
			glog.Infof("Finishing testing...")
			return nil
		}
	}
	return nil
}
