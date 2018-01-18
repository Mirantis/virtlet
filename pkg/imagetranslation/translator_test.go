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

package imagetranslation

import (
	"context"
	"testing"
)

// TestTranslations tests how image names are translated with various translation rules
func TestTranslations(t *testing.T) {
	configs := map[string]ImageTranslation{
		"config1": {
			Rules: []TranslationRule{
				{
					Regex: `^image(\d+)`,
					Url:   "http://example.net/image_$1.qcow2",
				},
				{
					Regex: `image(\d+)`,
					Url:   "http://example.net/alt_$1.qcow2",
				},
				{
					Name: "image1",
					Url:  "https://example.net/base.qcow2",
				},
			},
		},
		"config2": {
			Prefix: "prod",
			Rules: []TranslationRule{
				{
					Regex: `^linux/(\d+\.\d+)`,
					Url:   "http://acme.org/linux_$1.qcow2",
				},
				{
					Name: "linux/1",
					Url:  "https://acme.org/linux.qcow2",
				},
			},
		},
	}

	translator := NewImageNameTranslator().(*imageNameTranslator)
	translator.LoadConfigs(context.Background(), NewFakeConfigSource(configs))

	for _, tc := range []struct {
		name        string
		allowRegexp bool
		imageName   string
		expectedUrl string
	}{
		{
			name:        "strict translation",
			allowRegexp: false,
			imageName:   "image1",
			expectedUrl: "https://example.net/base.qcow2",
		},
		{
			name:        "negative strict translation",
			allowRegexp: false,
			imageName:   "image2",
			expectedUrl: "image2",
		},
		{
			name:        "strict translation precedes regexps",
			allowRegexp: true,
			imageName:   "image1",
			expectedUrl: "https://example.net/base.qcow2",
		},
		{
			name:        "regexp translation",
			allowRegexp: true,
			imageName:   "image2",
			expectedUrl: "http://example.net/image_2.qcow2",
		},
		{
			name:        "negative regexp translation",
			allowRegexp: true,
			imageName:   "image",
			expectedUrl: "image",
		},
		{
			name:        "translation with prefix",
			allowRegexp: false,
			imageName:   "prod/linux/1",
			expectedUrl: "https://acme.org/linux.qcow2",
		},
		{
			name:        "regexp translation with prefix",
			allowRegexp: true,
			imageName:   "prod/linux/2.11",
			expectedUrl: "http://acme.org/linux_2.11.qcow2",
		},
		{
			name:        "negative translation with prefix",
			allowRegexp: false,
			imageName:   "prod/image1",
			expectedUrl: "prod/image1",
		},
		{
			name:        "empty string translation",
			allowRegexp: true,
			imageName:   "",
			expectedUrl: "",
		},
		{
			name:        "misleading translation with prefix",
			allowRegexp: true,
			imageName:   "prod/image1",
			expectedUrl: "http://example.net/alt_1.qcow2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			translator.AllowRegexp = tc.allowRegexp
			endpoint := translator.Translate(tc.imageName)
			if tc.expectedUrl != endpoint.Url {
				t.Errorf("expected URL %q, but got %q", tc.expectedUrl, endpoint.Url)
			}
		})
	}
}
