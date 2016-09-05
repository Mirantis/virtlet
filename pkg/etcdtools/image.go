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

package etcdtools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"golang.org/x/net/context"
)

func getKey(name string) string {
	hash := sha256.New()
	hash.Write([]byte(name))
	sum := hex.EncodeToString(hash.Sum(nil))
	return fmt.Sprintf("/image/%s", sum)
}

type ImageTool struct {
	keysAPITool *KeysAPITool
}

func NewImageEtcdTool(keysAPITool *KeysAPITool) *ImageTool {
	return &ImageTool{keysAPITool: keysAPITool}
}

func (i *ImageTool) SetImageFilepath(name, filepath string) error {
	kapi, err := i.keysAPITool.newKeysAPI()
	if err != nil {
		return err
	}

	key := getKey(name)
	_, err = kapi.Set(context.Background(), key, filepath, nil)
	if err != nil {
		return err
	}
	return nil
}

func (i *ImageTool) GetImageFilepath(name string) (string, error) {
	kapi, err := i.keysAPITool.newKeysAPI()
	if err != nil {
		return "", err
	}

	key := getKey(name)
	resp, err := kapi.Get(context.Background(), key, nil)
	if err != nil {
		return "", err
	}
	return resp.Node.Value, nil
}
