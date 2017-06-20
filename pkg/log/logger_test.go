/*
Copyright 2016-2017 Mirantis

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

package log

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hpcloud/tail"
)

func TestWorkerRunner_ListWorkers(t *testing.T) {
	// Setup.
	runner := newWorkerRunner()

	workerId := "sandbox01/container_0.log"
	runner.workers[workerId] = make(chan *tail.Line)

	// This is what we're testing here.
	workers := runner.ListWorkers()

	// Expectations.
	if len(workers) != 1 {
		t.Errorf("expected 1 worker, but got %d", len(workers))
	}
	if workers[0] != workerId {
		t.Errorf("expected '%s', but obtained '%s'", workerId, workers[0])
	}
}

func TestWorkerRunner_WorkerExists(t *testing.T) {
	// Setup.
	runner := newWorkerRunner()

	workerId := "sandbox01/container_0.log"
	runner.workers[workerId] = make(chan *tail.Line)

	// This is what we're testing here.
	answer := runner.WorkerExists(workerId)

	// Expectations.
	if !answer {
		t.Errorf("worker exists, but runner says it doesn't")
	}
}

func TestWorkerRunner_RunNewWorker(t *testing.T) {
	// Setup.
	baseDir, baseInDir, baseOutDir := setupDirStructure([]string{"sandbox01"}, "Foo Bar\n")
	defer os.RemoveAll(baseDir)

	runner := newWorkerRunner()
	statusCh := runner.InitStatusChannel()

	sandboxId := "sandbox01"
	inFile := filepath.Join(baseInDir, sandboxId, "container_0.log")
	outFile := filepath.Join(baseOutDir, sandboxId, "container_0.log")

	// This is what we're testing here.
	go runner.RunNewWorker(inFile, outFile, "workerId")
	waitWorkerStatus(statusCh, 1, "RUN", t)
	runner.StopAllWorkers()
	waitWorkerStatus(statusCh, 1, "STOP", t)

	// Expectations.
	verifyJsonLines(t, outFile, []map[string]interface{}{
		{
			"stream": "stdout",
			"log":    "Foo Bar\n",
		},
	})
}

func TestWorkerRunner_RunNewWorker_Twice(t *testing.T) {
	// Setup.
	baseDir, baseInDir, baseOutDir := setupDirStructure([]string{"sandbox01"}, "Foo Bar\n")
	defer os.RemoveAll(baseDir)

	runner := newWorkerRunner()
	statusCh := runner.InitStatusChannel()

	sandboxId := "sandbox01"
	inFile := filepath.Join(baseInDir, sandboxId, "container_0.log")
	outFile := filepath.Join(baseOutDir, sandboxId, "container_0.log")

	// This is what we're testing here.
	go runner.RunNewWorker(inFile, outFile, sandboxId)
	waitWorkerStatus(statusCh, 1, "RUN", t)
	runner.StopAllWorkers()
	waitWorkerStatus(statusCh, 1, "STOP", t)

	go runner.RunNewWorker(inFile, outFile, sandboxId)
	waitWorkerStatus(statusCh, 1, "RUN", t)
	runner.StopAllWorkers()
	waitWorkerStatus(statusCh, 1, "STOP", t)

	// Expectations.
	verifyJsonLines(t, outFile, []map[string]interface{}{
		{
			"stream": "stdout",
			"log":    "Foo Bar\n",
		},
	})
}

func TestWorkerRunner_RunNewWorker_Append(t *testing.T) {
	// Setup.
	baseDir, baseInDir, baseOutDir := setupDirStructure([]string{"sandbox01"}, "Foo Bar\n")
	defer os.RemoveAll(baseDir)

	runner := newWorkerRunner()
	statusCh := runner.InitStatusChannel()

	sandboxId := "sandbox01"
	inFile := filepath.Join(baseInDir, sandboxId, "container_0.log")
	outFile := filepath.Join(baseOutDir, sandboxId, "container_0.log")

	// This is what we're testing here.
	go runner.RunNewWorker(inFile, outFile, sandboxId)
	waitWorkerStatus(statusCh, 1, "RUN", t)
	f, _ := os.OpenFile(inFile, os.O_APPEND|os.O_WRONLY, 0777)
	f.WriteString("Append Line 1\n")
	f.Sync()
	f.WriteString("Append Line 2\n")
	f.Sync()
	waitWorkerStatus(statusCh, 2, "RUN", t)
	runner.StopAllWorkers()
	waitWorkerStatus(statusCh, 1, "STOP", t)

	verifyJsonLines(t, outFile, []map[string]interface{}{
		{
			"stream": "stdout",
			"log":    "Foo Bar\n",
		},
		{
			"stream": "stdout",
			"log":    "Append Line 1\n",
		},
		{
			"stream": "stdout",
			"log":    "Append Line 2\n",
		},
	})
}

func TestVirtletLogger_SpawnWorkers(t *testing.T) {
	// Setup.
	baseDir, baseInDir, baseOutDir := setupDirStructure([]string{"sandbox01"}, "Foo Bar\n")
	defer os.RemoveAll(baseDir)

	logger := newFakeVirtletLogger(baseInDir, baseOutDir)
	statusCh := logger.workerRunner.(*fakeWorkerRunner).InitStatusChannel()

	// This is what we're testing here.
	err := logger.SpawnWorkers()
	waitWorkerStatus(statusCh, 1, "RUN", t)

	// Expectations.
	if err != nil {
		t.Errorf("failed to spawn workers: %s", err)
	}

	workers := logger.workerRunner.ListWorkers()
	if len(workers) != 1 {
		t.Errorf("expected 1 worker but got %d", len(workers))
		t.FailNow()
	}
	if workers[0] != "sandbox01/container_0.log" {
		t.Errorf("expected worker 'sandbox01/container_0.log', but got '%s'", workers[0])
	}

	inFile := filepath.Join(baseInDir, "sandbox01", "container_0.log")
	outFile := filepath.Join(baseOutDir, "sandbox01", "container_0.log")
	expected := fmt.Sprintf("%s,%s", inFile, outFile)
	if logger.workerRunner.(*fakeWorkerRunner).WorkerFiles["sandbox01/container_0.log"] != expected {
		t.Errorf("expected WorkerFiles '%s', but got '%s'", expected, logger.workerRunner.(*fakeWorkerRunner).WorkerFiles["sandbox01"])
	}
}

func TestVirtletLogger_SpawnWorkers_MultipleAttempts(t *testing.T) {
	// Setup.
	baseDir, baseInDir, baseOutDir := setupDirStructure([]string{"sandbox01"}, "Foo Bar\n")
	defer os.RemoveAll(baseDir)

	secondAttemptFile := filepath.Join(baseInDir, "sandbox01", "container_1.log")
	ioutil.WriteFile(secondAttemptFile, []byte("Foo Bar\n"), 0777)

	thirdAttemptFile := filepath.Join(baseInDir, "sandbox01", "container_2.log")
	ioutil.WriteFile(thirdAttemptFile, []byte("Foo Bar\n"), 0777)

	logger := newFakeVirtletLogger(baseInDir, baseOutDir)
	statusCh := logger.workerRunner.(*fakeWorkerRunner).InitStatusChannel()

	// This is what we're testing here.
	err := logger.SpawnWorkers()
	waitWorkerStatus(statusCh, 3, "RUN", t)

	// Expectations.
	if err != nil {
		t.Errorf("failed to spawn workers: %s", err)
	}

	workers := logger.workerRunner.ListWorkers()
	if len(workers) != 3 {
		t.Errorf("expected 3 workers but got %d", len(workers))
		t.FailNow()
	}

	for i := 0; i < 3; i++ {
		filename := fmt.Sprintf("container_%d.log", i)
		inFile := filepath.Join(baseInDir, "sandbox01", filename)
		outFile := filepath.Join(baseOutDir, "sandbox01", filename)
		workerId := fmt.Sprintf("sandbox01/container_%d.log", i)

		expected := fmt.Sprintf("%s,%s", inFile, outFile)
		if logger.workerRunner.(*fakeWorkerRunner).WorkerFiles[workerId] != expected {
			t.Errorf("expected WorkerFiles '%s', but got '%s'", expected, logger.workerRunner.(*fakeWorkerRunner).WorkerFiles[workerId])
		}
	}
}

func TestVirtletLogger_StopObsoleteWorkers_RmOutputDir(t *testing.T) {
	// Setup.
	baseDir, baseInDir, baseOutDir := setupDirStructure([]string{"sandbox01", "sandbox02"}, "Foo Bar\n")
	defer os.RemoveAll(baseDir)

	logger := NewVirtletLogger(baseInDir, baseOutDir)
	statusCh := logger.(*virtletLogger).workerRunner.(*workerRunner).InitStatusChannel()

	logger.SpawnWorkers()
	defer logger.(*virtletLogger).workerRunner.StopAllWorkers()

	waitWorkerStatus(statusCh, 2, "RUN", t)

	// Setup: remove output dir while worker is running
	os.RemoveAll(filepath.Join(baseOutDir, "sandbox02"))

	// This is what we're testing here.
	logger.StopObsoleteWorkers()

	waitWorkerStatus(statusCh, 1, "STOP", t)

	// Expectations.
	if !logger.(*virtletLogger).workerRunner.WorkerExists("sandbox01/container_0.log") {
		t.Errorf("worker 'sandbox01/container_0.log' should still exist")
	}
	if logger.(*virtletLogger).workerRunner.WorkerExists("sandbox02/container_0.log") {
		t.Errorf("worker 'sandbox02/container_0.log' should not exist anymore")
	}
}

func TestVirtletLogger_StopObsoleteWorkers_RmInputDir(t *testing.T) {
	// Setup.
	baseDir, baseInDir, baseOutDir := setupDirStructure([]string{"sandbox01", "sandbox02"}, "Foo Bar\n")
	defer os.RemoveAll(baseDir)

	logger := NewVirtletLogger(baseInDir, baseOutDir)
	statusCh := logger.(*virtletLogger).workerRunner.(*workerRunner).InitStatusChannel()

	logger.SpawnWorkers()
	defer logger.(*virtletLogger).workerRunner.StopAllWorkers()

	waitWorkerStatus(statusCh, 2, "RUN", t)

	// Setup: remove output dir while worker is running
	os.RemoveAll(filepath.Join(baseInDir, "sandbox02"))

	// This is what we're testing here.
	logger.StopObsoleteWorkers()

	waitWorkerStatus(statusCh, 1, "STOP", t)

	// Expectations.
	if !logger.(*virtletLogger).workerRunner.WorkerExists("sandbox01/container_0.log") {
		t.Errorf("worker 'sandbox01/container_0.log' should still exist")
	}
	if logger.(*virtletLogger).workerRunner.WorkerExists("sandbox02/container_0.log") {
		t.Errorf("worker 'sandbox02/container_0.log' should not exist anymore")
	}
}

func setupDirStructure(sandboxIds []string, initialContent string) (string, string, string) {
	baseDir, _ := ioutil.TempDir("", "virtlet-log")

	baseInputDir := filepath.Join(baseDir, "input")
	baseOutputDir := filepath.Join(baseDir, "output")

	os.Mkdir(baseInputDir, 0777)
	os.Mkdir(baseOutputDir, 0777)

	for _, sandboxId := range sandboxIds {
		inputDir := filepath.Join(baseInputDir, sandboxId)
		outputDir := filepath.Join(baseOutputDir, sandboxId)

		os.Mkdir(inputDir, 0777)
		os.Mkdir(outputDir, 0777)

		inputFile := filepath.Join(inputDir, "container_0.log")
		ioutil.WriteFile(inputFile, []byte(initialContent), 0777)
	}

	return baseDir, baseInputDir, baseOutputDir
}

func verifyJsonLines(t *testing.T, filePath string, lines []map[string]interface{}) {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Errorf("output file should exist, but does not")
	}

	f, err := os.Open(filePath)
	if err != nil {
		t.Errorf("failed to open file: %s", filePath)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for n := 0; scanner.Scan(); n++ {
		l := scanner.Text()
		if n >= len(lines) {
			t.Errorf("excess line in the log: %q", l)
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Errorf("failed to unmarshal log line %q: %v", l, err)
			continue
		}
		timeStr, ok := m["time"].(string)
		if !ok {
			t.Errorf("bad/absent time in the log line %q", l)
			continue
		}
		logTime, err := time.Parse(time.RFC3339, timeStr)
		if err != nil {
			t.Errorf("failed to parse log time %q: %v", m["time"], err)
			continue
		}
		timeDiff := time.Now().Sub(logTime)
		if timeDiff < 0 || timeDiff > 10*time.Minute {
			t.Errorf("log time too far from now: %v", m["time"])
			continue
		}
		for k, v := range lines[n] {
			if m[k] != v {
				t.Errorf("bad %q value in log line %q: got %v, expected %v", k, l, m[k], v)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Errorf("error reading the output file: %v", err)
	}
}

func waitWorkerStatus(statusCh chan string, nWorkers int, status string, t *testing.T) {
	timeout := time.After(5 * time.Second)

	currN := 0

	for {
		select {
		case msg := <-statusCh:
			if strings.HasPrefix(msg, status) {
				currN += 1
			}
		case <-timeout:
			t.Errorf("timeout waiting for workers")
			t.FailNow()
			return
		}

		if currN >= nWorkers {
			return
		}

	}
}
