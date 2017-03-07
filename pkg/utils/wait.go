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

package utils

import (
	"fmt"
	"time"
)

// WaitLoop executes test func in loop until it returns error, true, or timeout passes
func WaitLoop(test func() (bool, error), retryPeriod time.Duration, timeout time.Duration) error {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(retryPeriod) {
		result, err := test()
		if err != nil {
			return err
		}
		if result {
			return nil
		}
	}

	return fmt.Errorf("timeout reached")
}
