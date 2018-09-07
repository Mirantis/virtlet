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
	"time"

	"github.com/Mirantis/virtlet/tests/e2e"
	"github.com/Mirantis/virtlet/tests/e2e/framework"
	"github.com/golang/glog"
)

type VMInstance struct {
	name           string
	ssh            framework.Executor
	vm             *framework.VMInterface
	controller     *framework.Controller
	testWaitTime   time.Duration
	lifetime       time.Duration
	failures       int
	testTicker     *time.Ticker
	lifetimeTicker *time.Ticker
}

func (i *VMInstance) Test(ctx context.Context, instance *VMInstance, testFunc func(*VMInstance) error, errCh chan error) {
	defer instance.Stop()
	var err error
	for {
		select {
		case <-ctx.Done():
			glog.V(3).Infof("Testing '%s' VM stopped", instance.name)
			return
		case <-instance.testTicker.C:
			glog.V(3).Infof("Testing '%s' VM...", instance.name)
			err = testFunc(instance)
			if err != nil {
				glog.V(4).Infof("Test function failed with: %v, instance: %+v", err, instance)
				instance.failures++
				// there are mostly network tests so we allow one glitch
				if instance.failures > 1 {
					errCh <- fmt.Errorf("Testing VM %s failed: %v", instance.name, err)
					return
				}
			}
		case <-instance.lifetimeTicker.C:
			glog.V(4).Infof("Recreating VM: %s", instance.name)
			err = instance.ReCreate()
			if err != nil {
				glog.V(4).Infof("Recreating VM %s failed: %v", instance.name, err)
				errCh <- fmt.Errorf("Failed to recreate VM %s: %v", instance.name, err)
				return
			}
		}
	}
}

func (i *VMInstance) Create() error {
	var err error

	i.vm = i.controller.VM(i.name)
	err = i.vm.CreateAndWait(e2e.VMOptions{}.ApplyDefaults(), time.Minute*5, nil)
	if err != nil {
		return err
	}

	err, i.ssh = waitSSH(i.vm)
	if err != nil {
		return err
	}
	i.lifetimeTicker = time.NewTicker(i.lifetime)
	i.testTicker = time.NewTicker(i.testWaitTime)
	return nil
}

func (i *VMInstance) Delete() error {
	err := i.vm.Delete(30 * time.Second)
	if err != nil {
		return err
	}
	return nil
}

func (i *VMInstance) ReCreate() error {
	err := i.Delete()
	if err != nil {
		return err
	}
	return i.Create()
}

func waitSSH(vm *framework.VMInterface) (error, framework.Executor) {
	var err error
	var ssh framework.Executor
	for i := 0; i < 60*5; i += 3 {
		time.Sleep(3 * time.Second)
		ssh, err = vm.SSH(e2e.DefaultSSHUser, e2e.SshPrivateKey)
		if err != nil {
			continue
		}
		_, err = framework.RunSimple(ssh)
		if err != nil {
			continue
		}
		return nil, ssh
	}
	return fmt.Errorf("Timeout waiting for ssh connection to vm: %v", vm), nil
}

func (i *VMInstance) Stop() {
	i.lifetimeTicker.Stop()
	i.testTicker.Stop()
}
