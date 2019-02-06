/*
Copyright 2019 Mirantis

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

package fake

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/Mirantis/virtlet/pkg/utils"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

type fakeFileReader struct {
	rec      testutils.Recorder
	fileData string
}

var _ utils.FileReader = &fakeFileReader{}

// ReadString implements ReadString method of utils.FileReader interface
func (fr *fakeFileReader) ReadString(delim byte) (line string, err error) {
	lines := strings.SplitN(fr.fileData, string(delim), 1)
	line = lines[0]
	if len(lines) > 1 {
		fr.fileData = lines[1]
	} else {
		err = io.EOF
	}
	fr.rec.Rec("ReadString: "+line, err)
	return
}

// Close implements Close method of utils.FileReader interface
func (fr *fakeFileReader) Close() error {
	return nil
}

type fakeFilesManipulator struct {
	readerData map[string]string
	rec        testutils.Recorder
}

var _ utils.FilesManipulator = &fakeFilesManipulator{}

// FileReader implements the FileReader method of FilesManipulator interface
func (fm *fakeFilesManipulator) FileReader(path string) (utils.FileReader, error) {
	data, ok := fm.readerData[path]
	if !ok {
		fm.rec.Rec("FileReader - undefined path", path)
		return nil, &os.PathError{Op: "open", Path: path, Err: errors.New("file not found")}
	}
	fm.rec.Rec("FileReader", path)
	return &fakeFileReader{rec: fm.rec, fileData: data}, nil
}

// WriteFile implements the WriteFile method of FilesManipulator interface
func (fm *fakeFilesManipulator) WriteFile(path string, data []byte, perm os.FileMode) error {
	fm.rec.Rec("WriteFile: "+path, string(data))
	return nil
}

// NewFakeFilesManipulator returns fakeFilesManipulator instance as utils.FilesManipulator interface
func NewFakeFilesManipulator(rec testutils.Recorder, files map[string]string) utils.FilesManipulator {
	return &fakeFilesManipulator{rec: rec, readerData: files}
}
