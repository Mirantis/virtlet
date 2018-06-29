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
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	"github.com/golang/glog"
)

func testVM(instance *VMInstance) error {
	err := checkDefaultRoute(instance)
	if err != nil {
		return fmt.Errorf("Checking default route failed: %v", err)
	}
	err = checkInternetConnectivity(instance)
	if err != nil {
		return fmt.Errorf("Checking internet connectivity failed: %v", err)
	}
	err = checkInterPodConnectivity(instance)
	if err != nil {
		return fmt.Errorf("Checking inter-pod connectivity failed: %v", err)
	}
	return nil
}

func checkDefaultRoute(instance *VMInstance) error {
	vmPod, err := instance.vm.Pod()
	if err != nil {
		return err
	}

	glog.V(4).Infof("Should have default route")
	out, err := framework.RunSimple(instance.ssh, "ip r")
	if err != nil {
		return err
	}
	if !strings.Contains(out, "default via") {
		return fmt.Errorf("Should contain `default via` line but it's missing")
	}
	if !strings.Contains(out, "src "+vmPod.Pod.Status.PodIP) {
		return fmt.Errorf("Should contain `src %s` line but it's missing", vmPod.Pod.Status.PodIP)
	}
	return nil
}

func checkInternetConnectivity(instance *VMInstance) error {
	glog.V(4).Infof("Should have internet connectivity")
	output, err := framework.RunSimple(instance.ssh, "ping -c1 8.8.8.8")
	if err != nil {
		return fmt.Errorf("Error when running command ping -c1 8.8.8.8: %v", err)
	}
	matched, err := regexp.MatchString("1 .*transmitted, 1 .*received, 0% .*loss", output)
	if err != nil {
		return fmt.Errorf("Error when running regexp: %v", err)
	}
	if !matched {
		return fmt.Errorf("No internet connectivity. ping output: ```%s```", output)
	}
	return nil
}

func checkInterPodConnectivity(instance *VMInstance) error {
	glog.V(4).Infof("Should be able to access another k8s endpoint")
	cmd := fmt.Sprintf("curl -s --connect-timeout 5 http://nginx.%s.svc.cluster.local", instance.controller.Namespace())
	out, err := framework.RunSimple(instance.ssh, cmd)
	if err != nil {
		return fmt.Errorf("Error when running curl: %v", err)
	}
	if !strings.Contains(out, "Thank you for using nginx.") {
		return fmt.Errorf("Should contain `Thank you for using nginx.` line but it's missing")
	}
	return nil
}

func checkLogs(instance *VMInstance) error {
	//FIXME: doesn't work
	/*
		ctx, closeFunc := context.WithCancel(context.Background())
		defer closeFunc()
		localExecutor := framework.LocalExecutor(ctx)

		fmt.Println("Should update logs")
		var stdout bytes.Buffer
		ok := false
		for i := 0; i < 10; i++ {
			time.Sleep(1)

			message := "TESTTEXT"
			stdin := bytes.NewBufferString("\n\nTESTTEXT\n\n")

			stdout.Reset()
			err = localExecutor.Run(stdin, &stdout, stdout, "kubectl", "-n", controller.Namespace(), "attach", "-i", vm.Name)
			if err != nil {
				err = fmt.Errorf("Failed to run `kubectl -n %s attach -i %s: %v. Stdout: %s", controller.Namespace(), vm.Name, err, stdout.String())
				continue
			}

			stdout.Reset()
			err = localExecutor.Run(nil, &stdout, &stdout, "kubectl", "-n", controller.Namespace(), "logs", vm.Name)
			if err != nil {
				err = fmt.Errorf("Failed to run `kubectl -n %s logs %s: %v. Stdout: %s", controller.Namespace(), vm.Name, err, stdout.String())
				continue
			}

			if !strings.Contains(stdout.String(), message) {
				err = fmt.Errorf("Logs should contain `%s` message but it's missing", message)
				continue
			}
			ok = true
			break
		}
		if !ok {
			return err
		}
	*/
	return nil
}

func startNginxPod(controller *framework.Controller) (*framework.PodInterface, error) {
	// Create a Pod to test in-cluster network connectivity
	p, err := controller.RunPod("nginx", "nginx", nil, time.Minute*4, 80)
	if err != nil {
		return nil, err
	}

	return p, nil
}
