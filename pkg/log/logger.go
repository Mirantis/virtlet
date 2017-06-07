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

package log

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/golang/glog"
	"github.com/hpcloud/tail"
)

type VirtletLogger interface {
	SpawnWorkers() error
	StopObsoleteWorkers()
	StopAllWorkers()
}

type WorkerRunner interface {
	RunNewWorker(inputFile, outputFile, workerId string)
	ListWorkers() []string
	WorkerExists(workerId string) bool
	StopWorker(workerId string)
	StopAllWorkers()
	InitStatusChannel() chan string
}

var _ VirtletLogger = (*virtletLogger)(nil)
var _ WorkerRunner = (*workerRunner)(nil)

func NewVirtletLogger(virtletDir, kubernetesDir string) VirtletLogger {
	logger := &virtletLogger{
		config: virtletLoggerConf{
			VirtletDir:    virtletDir,
			KubernetesDir: kubernetesDir,
		},
		workerRunner: newWorkerRunner(),
	}
	return logger
}

type virtletLoggerConf struct {
	VirtletDir    string
	KubernetesDir string
}

type virtletLogger struct {
	config       virtletLoggerConf
	workerRunner WorkerRunner
}

func (v *virtletLogger) SpawnWorkers() error {
	glog.V(1).Infoln("Check and spawn workers")

	vmDirs, err := ioutil.ReadDir(v.config.VirtletDir)
	if err != nil {
		return err
	}

	for _, vmDir := range vmDirs {
		sandboxId := vmDir.Name()

		if !vmDir.IsDir() {
			continue
		}

		vmLogFiles, err := ioutil.ReadDir(filepath.Join(v.config.VirtletDir, sandboxId))
		if err != nil {
			glog.Warningf("Failed to read sandbox '%s' dir: %s", sandboxId, err.Error())
			continue
		}

		for _, vmLogFile := range vmLogFiles {
			filename := vmLogFile.Name()
			inputFile := filepath.Join(v.config.VirtletDir, sandboxId, filename)
			outputFile := filepath.Join(v.config.KubernetesDir, sandboxId, filename)

			if _, err := os.Stat(filepath.Dir(outputFile)); os.IsNotExist(err) {
				glog.V(1).Infof("Kubernetes directory '%s' does not exist: %s", filepath.Dir(outputFile), err.Error())
				break
			}

			go v.workerRunner.RunNewWorker(inputFile, outputFile, fmt.Sprintf("%s/%s", sandboxId, filename))
		}
	}

	return nil
}

func (v *virtletLogger) StopObsoleteWorkers() {
	glog.V(1).Infoln("Stop obsolete workers")
	for _, workerId := range v.workerRunner.ListWorkers() {
		if _, err := os.Stat(filepath.Join(v.config.KubernetesDir, workerId)); os.IsNotExist(err) {
			v.workerRunner.StopWorker(workerId)
			continue
		}
		if _, err := os.Stat(filepath.Join(v.config.VirtletDir, workerId)); os.IsNotExist(err) {
			v.workerRunner.StopWorker(workerId)
		}
	}
}

func (v *virtletLogger) StopAllWorkers() {
	v.workerRunner.StopAllWorkers()
}

const STOP string = "---LOGGER-STOP---"

type workerRunner struct {
	workers    map[string]chan *tail.Line // map[sandboxId]chan
	workersMux sync.Mutex
	statusCh   chan string
}

func newWorkerRunner() *workerRunner {
	return &workerRunner{
		workers:  make(map[string]chan *tail.Line),
		statusCh: nil,
	}
}

func (w *workerRunner) RunNewWorker(inputFile, outputFile, workerId string) {
	glog.V(1).Infoln("Spawned worker", workerId)

	w.reportWorkerState("START", workerId)
	defer w.reportWorkerState("STOP", workerId)

	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		file, err := os.Create(outputFile)
		if err != nil {
			glog.Warningln("Failed to create output file:", err)
			return
		}
		file.Close()
	}

	f, err := os.OpenFile(outputFile, os.O_WRONLY, 0777)
	if err != nil {
		glog.V(1).Infoln("Failed to open output file:", err)
		return
	}
	defer f.Close()

	// Tail VM's file.
	t, err := tail.TailFile(inputFile, tail.Config{Follow: true, ReOpen: true})
	if err != nil {
		glog.V(1).Infoln("Failed to tail input file:", err)
		return
	}

	// Expose channel to virtletLogger so that it can close it when needed.
	w.registerWorker(workerId, t.Lines)

	// Do work. This forloop will block until canceled; it will wait for new
	// lines to come and parse them immediately.
	for line := range t.Lines {
		if line.Text == STOP {
			break
		}

		if line.Err != nil {
			glog.V(1).Infoln("Error reading line:", line.Err.Error())
			break
		}

		// Convert raw line into Kubernetes json.
		m := map[string]interface{}{
			"time":   line.Time.Format(time.RFC3339),
			"stream": "stdout",
			"log":    line.Text + "\n",
		}
		converted, err := json.Marshal(m)
		if err != nil {
			glog.V(1).Infoln("Error marshalling the log line:", line.Err.Error())
			// this must be something exceptional, but let's not stop logging here
			continue
		}
		if _, err = f.Write(append(converted, '\n')); err != nil {
			glog.V(1).Infoln("Error writing log line:", line.Err.Error())
			break
		}
		if err = f.Sync(); err != nil {
			glog.V(1).Infoln("Error syncing the log file:", line.Err.Error())
			break
		}

		w.reportWorkerState("RUN", workerId)
	}

	// This code is only executed when t.Lines channel is closed.
	delete(w.workers, workerId)
	glog.V(1).Infof("Worker for sandbox '%s' stopped gracefully", workerId)
}

func (w *workerRunner) ListWorkers() (sandboxIds []string) {
	for sandboxId, _ := range w.workers {
		sandboxIds = append(sandboxIds, sandboxId)
	}
	return sandboxIds
}

func (w *workerRunner) WorkerExists(workerId string) bool {
	w.workersMux.Lock()
	defer w.workersMux.Unlock()
	return w.workers[workerId] != nil
}

func (w *workerRunner) StopWorker(workerId string) {
	if w.workers[workerId] != nil {
		glog.V(1).Infof("Stop worker '%s'", workerId)
		w.workersMux.Lock()
		defer w.workersMux.Unlock()
		w.workers[workerId] <- &tail.Line{Text: STOP}
	}
}

func (w *workerRunner) StopAllWorkers() {
	glog.V(1).Infoln("Stop all workers")
	for workerId, _ := range w.workers {
		w.StopWorker(workerId)
	}

	for {
		if len(w.workers) == 0 {
			return
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func (w *workerRunner) InitStatusChannel() chan string {
	if w.statusCh == nil {
		w.statusCh = make(chan string, 100)
	}
	return w.statusCh
}

func (w *workerRunner) reportWorkerState(state, workerId string) {
	msg := fmt.Sprintf("%s_SEP_%s", state, workerId)
	if w.statusCh != nil {
		w.statusCh <- msg
	}
}

func (w *workerRunner) registerWorker(workerId string, ch chan *tail.Line) {
	w.workersMux.Lock()
	defer w.workersMux.Unlock()
	w.workers[workerId] = ch
}
