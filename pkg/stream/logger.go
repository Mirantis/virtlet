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

package stream

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"

	"github.com/golang/glog"
)

// NewLogWritter writes the lines from stdout channel in logFile in k8s format
func NewLogWriter(stdout <-chan []byte, logFile string, wg *sync.WaitGroup) {
	defer wg.Done()
	glog.V(1).Info("Spawned new log writer. Log file:", logFile)
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		file, err := os.Create(logFile)
		if err != nil {
			glog.Warningln("Failed to create output file:", err)
			return
		}
		file.Close()
	}

	f, err := os.OpenFile(logFile, os.O_WRONLY, 0777)
	if err != nil {
		glog.Error("Failed to open output file:", err)
		return
	}
	defer f.Close()

	buffer := bytes.NewBufferString("")
	for data := range stdout {
		buffer.Write(data)
		for {
			line, err := buffer.ReadString('\n')
			if err != nil {
				// if EOF then write data back to buffer. It's unfinished line
				if err == io.EOF {
					buffer.WriteString(line)
					break
				} else {
					glog.Error("Error when reading from buffer:", err)
				}

			}
			err = writeLog(f, line)
			if err != nil {
				break
			}
		}
	}
	glog.V(1).Info("Log writter stopped. Finished logging to file:", logFile)
}

func writeLog(f *os.File, line string) error {
	// Convert raw line into Kubernetes json.
	m := map[string]interface{}{
		"time":   time.Now().Format(time.RFC3339),
		"stream": "stdout",
		"log":    line,
	}
	converted, err := json.Marshal(m)
	if err != nil {
		glog.Warning("Error marshalling the log line:", err)
		// this must be something exceptional, but let's not stop logging here
		return nil
	}
	if _, err = f.Write(append(converted, '\n')); err != nil {
		glog.V(1).Info("Error writing log line:", err)
		return err
	}
	if err = f.Sync(); err != nil {
		glog.V(1).Info("Error syncing the log file:", err)
		return err
	}
	return nil
}
