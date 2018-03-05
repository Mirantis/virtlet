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

package utils

import (
	"log"

	uuid "github.com/nu7hatch/gouuid"
)

// NewUUID returns a new uuid4 as a string
func NewUUID() string {
	u, err := uuid.NewV4()
	if err != nil {
		log.Panicf("can't generate new uuid4: %v", err)
	}
	return u.String()
}

// NewUUID5 returns a new uuid5 as a string
func NewUUID5(nsUUID, s string) string {
	ns, err := uuid.ParseHex(nsUUID)
	if err != nil {
		log.Panicf("can't parse namespace uuid: %v", err)
	}
	u, err := uuid.NewV5(ns, []byte(s))
	if err != nil {
		log.Panicf("can't generate new uuid5: %v", err)
	}
	return u.String()
}
