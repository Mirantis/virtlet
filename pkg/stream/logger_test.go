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
	"bufio"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func setupTmpLogFile(sandboxId string) string {
	baseDir, _ := ioutil.TempDir("", "virtlet-log")
	outputFile := filepath.Join(baseDir, "output.log")
	os.Mkdir(baseDir, 0777)
	return outputFile
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

func TestLoggingInNewLogWritter(t *testing.T) {
	var wg sync.WaitGroup

	cases := []struct {
		name       string
		outputFile string
		c          chan []byte
		lines      [][]byte
		jsonLines  []map[string]interface{}
	}{
		{
			name:       "One line",
			outputFile: setupTmpLogFile("test1"),
			c:          make(chan []byte),
			lines:      [][]byte{[]byte("test")},
			jsonLines: []map[string]interface{}{
				{
					"stream": "stdout",
					"log":    "test\n",
				},
			},
		},
		{
			name:       "Many lines in one message",
			outputFile: setupTmpLogFile("test1"),
			c:          make(chan []byte),
			lines:      [][]byte{[]byte("test\ntest2\n")},
			jsonLines: []map[string]interface{}{
				{
					"stream": "stdout",
					"log":    "test\n",
				},
				{
					"stream": "stdout",
					"log":    "test2\n",
				},
			},
		},
		{
			name:       "Many messages",
			outputFile: setupTmpLogFile("test1"),
			c:          make(chan []byte),
			lines:      [][]byte{[]byte("test\n"), []byte("test2\n")},
			jsonLines: []map[string]interface{}{
				{
					"stream": "stdout",
					"log":    "test\n",
				},
				{
					"stream": "stdout",
					"log":    "test2\n",
				},
			},
		},
		{
			name:       "No messages",
			outputFile: setupTmpLogFile("test1"),
			c:          make(chan []byte),
			lines:      [][]byte{},
			jsonLines:  []map[string]interface{}{},
		},
	}

	for _, test := range cases {
		t.Logf("Running `%s` test", test.name)
		defer os.RemoveAll(test.outputFile)
		wg.Add(1)
		go NewLogWriter(test.c, test.outputFile, &wg)
		for _, line := range test.lines {
			test.c <- line
		}
		close(test.c)
		wg.Wait()

		verifyJsonLines(t, test.outputFile, test.jsonLines)
	}
}
