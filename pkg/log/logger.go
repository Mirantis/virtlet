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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/hpcloud/tail"
)

type VirtletLogger interface {
	SpawnWorkers() error
	StopObsoleteWorkers()
}

type WorkerRunner interface {
	RunNewWorker(inputFile, outputFile, sandboxId string)
	ListWorkers() []string
	WorkerExists(sandboxId string) bool
	StopWorker(sandboxId string)
	StopAllWorkers()
	InitStatusChannel() chan string
}

var _ VirtletLogger = (*virtletLogger)(nil)
var _ WorkerRunner = (*workerRunner)(nil)

func NewVirtletLogger(virtletFolder, kubernetesFolder string) VirtletLogger {
	logger := &virtletLogger{
		config: virtletLoggerConf{
			VirtletFolder:      virtletFolder,
			VirtletFilename:    "raw.log",
			KubernetesFolder:   kubernetesFolder,
			KubernetesFilename: "_0.log",
		},
		workerRunner: newWorkerRunner(),
	}
	return logger
}

type virtletLoggerConf struct {
	VirtletFolder      string
	VirtletFilename    string
	KubernetesFolder   string
	KubernetesFilename string
}

type virtletLogger struct {
	config       virtletLoggerConf
	workerRunner WorkerRunner
}

func (v *virtletLogger) SpawnWorkers() error {
	fmt.Println("Check and spawn workers")

	vmFolders, err := ioutil.ReadDir(v.config.VirtletFolder)
	if err != nil {
		return err
	}

	for _, vmFolder := range vmFolders {
		sandboxId := vmFolder.Name()

		if v.workerRunner.WorkerExists(sandboxId) {
			fmt.Printf("worker for sandbox '%s' already running. Skip.\n", sandboxId)
			continue
		}

		inputFile := filepath.Join(v.config.VirtletFolder, sandboxId, v.config.VirtletFilename)
		outputFile := filepath.Join(v.config.KubernetesFolder, sandboxId, v.config.KubernetesFilename)
		if !vmFolder.IsDir() {
			continue
		}
		if _, err := os.Stat(inputFile); os.IsNotExist(err) {
			continue
		}
		if _, err := os.Stat(filepath.Dir(outputFile)); os.IsNotExist(err) {
			fmt.Printf("found '%s' for converting, but Kubernetes does not expect it\n", filepath.Dir(outputFile))
			continue
		}

		go v.workerRunner.RunNewWorker(inputFile, outputFile, sandboxId)
	}

	return nil
}

func (v *virtletLogger) StopObsoleteWorkers() {
	fmt.Println("Stop obsolete workers")
	for _, sandboxId := range v.workerRunner.ListWorkers() {
		if _, err := os.Stat(filepath.Join(v.config.KubernetesFolder, sandboxId)); os.IsNotExist(err) {
			v.workerRunner.StopWorker(sandboxId)
			continue
		}
		if _, err := os.Stat(filepath.Join(v.config.VirtletFolder, sandboxId)); os.IsNotExist(err) {
			v.workerRunner.StopWorker(sandboxId)
		}
	}
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

func (w *workerRunner) RunNewWorker(inputFile, outputFile, sandboxId string) {
	fmt.Println("Spawned worker for:", sandboxId)

	w.reportWorkerState("START", sandboxId)
	defer w.reportWorkerState("STOP", sandboxId)

	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		file, err := os.Create(outputFile)
		if err != nil {
			fmt.Println("failed to create output file:", err)
			return
		}
		file.Close()
	}

	f, err := os.OpenFile(outputFile, os.O_WRONLY, 0777)
	if err != nil {
		fmt.Println("failed to open output file:", err)
		return
	}
	defer f.Close()

	// Tail VM's file.
	t, err := tail.TailFile(inputFile, tail.Config{Follow: true, ReOpen: true})
	if err != nil {
		fmt.Println("failed to tail input file:", err)
		return
	}

	// Expose channel to virtletLogger so that it can close it when needed.
	w.registerWorker(sandboxId, t.Lines)

	// Do work. This forloop will block until canceled; it will wait for new
	// lines to come and parse them immediately.
	for line := range t.Lines {
		if line.Text == STOP {
			break
		}

		if line.Err != nil {
			fmt.Println("error reading line:", line.Err.Error())
			break
		}

		// Convert raw line into Kubernetes json.
		converted := fmt.Sprintf(`{"time": "%s", "stream": "stdout","log":"%s\n"}`, line.Time.Format(time.RFC3339), escapeLine(line.Text))
		converted = converted + "\n"

		f.WriteString(converted)
		f.Sync()

		w.reportWorkerState("RUN", sandboxId)
	}

	// This code is only executed when t.Lines channel is closed.
	delete(w.workers, sandboxId)
	fmt.Printf("Worker for sandbox '%s' stopped gracefully\n", sandboxId)
}

func (w *workerRunner) ListWorkers() (sandboxIds []string) {
	for sandboxId, _ := range w.workers {
		sandboxIds = append(sandboxIds, sandboxId)
	}
	return sandboxIds
}

func (w *workerRunner) WorkerExists(sandboxId string) bool {
	w.workersMux.Lock()
	defer w.workersMux.Unlock()
	return w.workers[sandboxId] != nil
}

func (w *workerRunner) StopWorker(sandboxId string) {
	if w.workers[sandboxId] != nil {
		fmt.Printf("Stop worker '%s'\n", sandboxId)
		w.workersMux.Lock()
		defer w.workersMux.Unlock()
		w.workers[sandboxId] <- &tail.Line{Text: STOP}
	}
}

func (w *workerRunner) StopAllWorkers() {
	fmt.Println("Stop all workers")
	for sandboxId, _ := range w.workers {
		w.StopWorker(sandboxId)
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

func (w *workerRunner) reportWorkerState(state, sandboxId string) {
	msg := fmt.Sprintf("%s_SEP_%s", state, sandboxId)
	if w.statusCh != nil {
		w.statusCh <- msg
	}
}

func (w *workerRunner) registerWorker(sandboxId string, ch chan *tail.Line) {
	w.workersMux.Lock()
	defer w.workersMux.Unlock()
	w.workers[sandboxId] = ch
}

func escapeLine(line string) string {
	line = strings.TrimRightFunc(line, unicode.IsSpace)
	line = strings.Replace(line, "\"", "\\\"", -1)
	return line
}
