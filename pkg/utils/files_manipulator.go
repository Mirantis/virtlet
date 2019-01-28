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

package utils

import (
	"bufio"
	"io/ioutil"
	"os"
)

// FilesManipulator provides an interface to files manipulation
type FilesManipulator interface {
	// FileReader given a path returns fileReader instance for data under
	// that path or any occured during the operation error.
	FileReader(path string) (FileReader, error)
	// WriteFile given a path creates new file or truncates
	// existing one, setting for it provided permissions and filling it
	// with provided data. Returns an error if any occured
	// during the operation.
	WriteFile(path string, data []byte, perm os.FileMode) error
}

type realFilesManipulator struct{}

var DefaultFilesManipulator FilesManipulator = &realFilesManipulator{}

// FileReader provides an interface to file reading
type FileReader interface {
	// ReadString returns next part of data up to (and including it)
	// delimeter byte.
	ReadString(delim byte) (string, error)
	// Close closes the reader.
	Close() error
}

type fileReader struct {
	f *os.File
	r *bufio.Reader
}

var _ FileReader = &fileReader{}

// ReadString implements the ReadString method of FileReader interface
func (fr *fileReader) ReadString(delim byte) (string, error) {
	return fr.r.ReadString(delim)
}

// Close implements the Close method of FileReader interface
func (fr *fileReader) Close() error {
	return fr.f.Close()
}

// FileReader implements the FileReader method of FilesManipulator interface
func (fm *realFilesManipulator) FileReader(path string) (FileReader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	f := &fileReader{f: file}

	r := bufio.NewReader(f.f)
	if err != nil {
		return nil, err
	}
	f.r = r
	return f, nil
}

// WriteFile implements the WriteFile method of FilesManipulator interface
func (fm *realFilesManipulator) WriteFile(path string, data []byte, perm os.FileMode) error {
	return ioutil.WriteFile(path, data, perm)
}
