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

package bolttools

import (
	"strings"
	"fmt"
	"github.com/boltdb/bolt"
)

func get(bucket *bolt.Bucket, key []byte) ([]byte, error) {
	value := bucket.Get(key)
	if value == nil {
		return nil, fmt.Errorf("key '%s' doesn't exist in the bucket", key)
	}

	return value, nil
}

func getString(bucket *bolt.Bucket, key string) (string, error) {
	value, err := get(bucket, []byte(key))
	if err != nil {
		return "", err
	}

	return string(value), nil
}

func DelValFromCommaSeparatedStrList(in string, del string) string {
	list := strings.Split(in, ",")

	for ind, el := range list {
		if el == del {
			list = append(list[:ind], list[(ind+1):]...)
			return strings.Join(list, ",")
		}
	}

	return in
}

func AddValToCommaSeparatedStrList(in string, add string) string {
	if len(in) == 0 {
		return add
	} else {
		return in + "," + add
	}
}
