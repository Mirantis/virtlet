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

	"github.com/hpcloud/tail"
)

func NewFakeVirtletLogger(virtletFolder, kubernetesFolder string) *fakeVirtletLogger {
	logger := &fakeVirtletLogger{}
	logger.config = virtletLoggerConf{
		VirtletFolder:      virtletFolder,
		VirtletFilename:    "raw.log",
		KubernetesFolder:   kubernetesFolder,
		KubernetesFilename: "_0.log",
	}

	fakeRunner := &fakeWorkerRunner{}
	fakeRunner.workers = make(map[string]chan *tail.Line)
	fakeRunner.WorkerFiles = make(map[string]string)
	logger.workerRunner = fakeRunner
	return logger
}

var _ VirtletLogger = (*fakeVirtletLogger)(nil)
var _ WorkerRunner = (*fakeWorkerRunner)(nil)

type fakeVirtletLogger struct {
	virtletLogger
}

type fakeWorkerRunner struct {
	workerRunner
	WorkerFiles map[string]string
}

func (w *fakeWorkerRunner) RunNewWorker(inputFile, outputFile, sandboxId string) {
	w.workers[sandboxId] = make(chan *tail.Line)
	w.WorkerFiles[sandboxId] = fmt.Sprintf("%s,%s", inputFile, outputFile)
	w.reportWorkerState("START", sandboxId)
	w.reportWorkerState("RUN", sandboxId)
}

func newFakeWorkerRunner() WorkerRunner {
	runner := &fakeWorkerRunner{}
	runner.workers = make(map[string]chan *tail.Line)
	return runner
}
