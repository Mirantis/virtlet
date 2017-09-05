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

package utils

import (
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/jonboulle/clockwork"
)

const (
	waitProcRetryPeriod = 200 * time.Millisecond
	waitProcTimeout     = 30 * time.Second
)

func readProcFile(procFile string) (int, uint64, error) {
	content, err := ioutil.ReadFile(procFile)
	if err != nil {
		return 0, 0, fmt.Errorf("reading procfile %q: %v", procFile, err)
	}
	parts := strings.Split(strings.TrimSpace(string(content)), " ")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("procfile %q is malformed: wrong number of fields", procFile)
	}
	pid, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("procfile %q is malformed: bad pid field", procFile)
	}
	startTime, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("procfile %q is malformed: bad start time field", procFile)
	}
	return pid, startTime, nil
}

func getProcessStartTime(pid int) (uint64, error) {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	content, err := ioutil.ReadFile(statPath)
	if err != nil {
		return 0, fmt.Errorf("error reading %q: %v", statPath, err)
	}
	text := string(content)
	// avoid problems due to spaces and parens in the executable name
	parenPos := strings.LastIndex(text, ")")
	if parenPos == -1 {
		return 0, fmt.Errorf("can't parse %q: no closing paren after executable filename", statPath)
	}
	parts := strings.Split(text[parenPos:], " ")
	if len(parts) < 21 {
		return 0, fmt.Errorf("can't parse %q: insufficient number of fields", statPath)
	}
	startTime, err := strconv.ParseUint(parts[20], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("can't parse %q: bad start time field", statPath)
	}
	return startTime, nil
}

// WaitForProcess waits for the following conditions to be true
// at the same time:
// * the specified procFile is readable and contains two numeric values separated by space,
//   PID and start time in jiffies (field 22 in /proc/PID/stat, starting from 1)
// * the process with the PID read from procFile exists and has start time
//   equal to the start time read from procFile
// This avoids possible problems with stale procFile that could happen
// if only PID was stored there.
// The command can be used in shell script to generate the "procfile"
// for the current shell:
// /bin/sh -c 'echo "$$ `cut -d" " -f22 /proc/$$/stat`"'
func WaitForProcess(procFile string) (int, error) {
	var pid int
	err := WaitLoop(func() (bool, error) {
		var expectedStartTime uint64
		var err error
		pid, expectedStartTime, err = readProcFile(procFile)
		if err != nil {
			glog.V(3).Infof("procfile %q not ready yet: %v", procFile, err)
			return false, nil
		}
		startTime, err := getProcessStartTime(pid)
		if err != nil {
			glog.V(3).Infof("procfile %q: can't get process start time for pid %d yet: %v", procFile, pid, err)
			return false, nil
		}
		if startTime != expectedStartTime {
			glog.V(3).Infof("procfile %q is stale: start time for pid %d is %d instead of %d", procFile, pid, startTime, expectedStartTime)
			return false, nil
		}
		return true, nil
	}, waitProcRetryPeriod, waitProcTimeout, clockwork.NewRealClock())
	if err != nil {
		return 0, err
	}
	return pid, nil
}
