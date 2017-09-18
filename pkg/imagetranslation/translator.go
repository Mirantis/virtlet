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
	"os"
	"regexp"
	"strings"

	"github.com/golang/glog"

	"github.com/Mirantis/virtlet/pkg/utils"
)

type imageNameTranslator struct {
	AllowRegexp bool

	translations map[string]*ImageTranslation
}

// LoadConfigs implements ImageNameTranslator LoadConfigs
func (t *imageNameTranslator) LoadConfigs(sources ...ConfigSource) {
	translations := map[string]*ImageTranslation{}
	for _, source := range sources {
		configs, err := source.Configs()
		if err != nil {
			glog.V(2).Infof("cannot get image translation configs from %s: %v", source.Description(), err)
			continue
		}
		for _, cfg := range configs {
			body, err := cfg.Payload()
			if err != nil {
				glog.V(2).Infof("cannot load image translation config %s from %s: %v", cfg.Name(), source.Description(), err)
				continue
			}

			translations[cfg.Name()] = &body
		}
	}
	t.translations = translations
}

// Translate implements ImageNameTranslator Translate
func (t *imageNameTranslator) Translate(name string) utils.Endpoint {
	for _, translation := range t.translations {
		prefix := translation.Prefix + "/"
		unprefixedName := name
		if prefix != "/" {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			unprefixedName = name[len(prefix):]
		}
		for _, r := range translation.Rules {
			if r.Name != "" && r.Name == unprefixedName {
				return r.Endpoint
			}
		}
		if !t.AllowRegexp {
			continue
		}
		for _, r := range translation.Rules {
			if r.Regex == "" {
				continue
			}
			re, err := regexp.Compile(r.Regex)
			if err != nil {
				glog.V(2).Infof("invalid regexp in image translation config: ", r.Regex)
				continue
			}
			submatchIndexes := re.FindStringSubmatchIndex(unprefixedName)
			if len(submatchIndexes) > 0 {
				r.Url = string(re.ExpandString(nil, r.Url, unprefixedName, submatchIndexes))
				return r.Endpoint
			}
		}
	}
	return utils.Endpoint{}
}

// NewImageNameTranslator creates an instance of ImageNameTranslator
func NewImageNameTranslator() ImageNameTranslator {
	env := strings.ToUpper(os.Getenv("IMAGE_REGEXP_TRANSLATION"))
	return &imageNameTranslator{
		AllowRegexp: env != "",
	}
}
