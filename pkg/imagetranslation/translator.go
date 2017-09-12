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
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	yaml "gopkg.in/yaml.v2"
)

type Endpoint struct {
	Url string `yaml:"url,omitempty"`
}

type TranslationRule struct {
	Name     string `yaml:"name,omitempty"`
	Regex    string `yaml:"regexp,omitempty"`
	Endpoint `yaml:",inline"`
}
type ImageTranslation struct {
	Prefix string            `yaml:"prefix,omitempty"`
	Rules  []TranslationRule `yaml:"translations"`

	timestamp time.Time
}

type TranslationConfig interface {
	Name() string
	Timestamp() (time.Time, error)
	Body() (ImageTranslation, error)
}

type ConfigSource interface {
	Configs() ([]TranslationConfig, error)
}

type fileConfigSource struct {
	configDirectory string
}

type fileConfig struct {
	name string
}

func (cs fileConfigSource) Configs() ([]TranslationConfig, error) {
	var result []TranslationConfig
	r, err := ioutil.ReadDir(cs.configDirectory)
	if err != nil {
		return nil, err
	}
	for _, f := range r {
		if f.IsDir() {
			continue
		}
		result = append(result, fileConfig{name: path.Join(cs.configDirectory, f.Name())})
	}
	return result, nil
}

func (c fileConfig) Name() string {
	return c.name
}

func (c fileConfig) Body() (ImageTranslation, error) {
	data, err := ioutil.ReadFile(c.name)
	var tr ImageTranslation
	if err != nil {
		return tr, err
	}
	err = yaml.Unmarshal(data, &tr)
	return tr, err
}

func (c fileConfig) Timestamp() (time.Time, error) {
	var result time.Time
	stat, err := os.Stat(c.name)
	if err != nil {
		return result, err
	}
	return stat.ModTime(), nil
}

func NewFileConfigSource(configDirectory string) ConfigSource {
	return fileConfigSource{configDirectory: configDirectory}
}

type ImageNameTranslator struct {
	AllowRegexp bool

	configSource          ConfigSource
	translations          map[string]*ImageTranslation
	lock                  sync.RWMutex
	stopBackgroundUpdates chan struct{}
}

func (t *ImageNameTranslator) load(config TranslationConfig) error {
	timestamp, err := config.Timestamp()
	if err != nil {
		return err
	}

	t.lock.RLock()
	currentTranslation := t.translations[config.Name()]
	t.lock.RUnlock()

	if currentTranslation != nil && currentTranslation.timestamp == timestamp {
		return nil
	}

	glog.Info("Loading image name translations from ", config.Name())
	tr, err := config.Body()
	tr.timestamp = timestamp

	t.lock.Lock()
	defer t.lock.Unlock()
	t.translations[config.Name()] = &tr
	glog.V(3).Infof("Loaded image translation config: %s", spew.Sdump(tr))
	return nil
}

func (t *ImageNameTranslator) LoadConfigs() error {
	configs, err := t.configSource.Configs()
	if err != nil {
		return err
	}

	unusedNames := map[string]bool{}
	t.lock.RLock()
	for key := range t.translations {
		unusedNames[key] = true
	}
	t.lock.RUnlock()

	for _, config := range configs {
		err := t.load(config)
		if err != nil {
			glog.Warning("Error loading image translation config: ", err)
		} else {
			delete(unusedNames, config.Name())
		}
	}

	if len(unusedNames) > 0 {
		t.lock.Lock()
		defer t.lock.Unlock()
		for name := range unusedNames {
			glog.Warning("Deleting image translation config ", name)
			delete(t.translations, name)
		}
	}
	return nil
}

func (t *ImageNameTranslator) Translate(name string) Endpoint {
	t.lock.RLock()
	defer t.lock.RUnlock()
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
				glog.Warning("Invalid regexp in image translation config: ", r.Regex)
				continue
			}
			submatchIndexes := re.FindStringSubmatchIndex(unprefixedName)
			if len(submatchIndexes) > 0 {
				r.Url = string(re.ExpandString(nil, r.Url, unprefixedName, submatchIndexes))
				return r.Endpoint
			}
		}
	}
	return Endpoint{}
}

func (t *ImageNameTranslator) StartBackgroundUpdates(pollFrequency time.Duration) {
	if t.stopBackgroundUpdates != nil {
		return
	}
	t.LoadConfigs()
	ticker := time.NewTicker(pollFrequency)
	t.stopBackgroundUpdates = make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				t.LoadConfigs()
			case <-t.stopBackgroundUpdates:
				ticker.Stop()
				close(t.stopBackgroundUpdates)
				t.stopBackgroundUpdates = nil
				return
			}
		}
	}()
}

func (t *ImageNameTranslator) StopBackgroundUpdates() {
	if t.stopBackgroundUpdates != nil {
		t.stopBackgroundUpdates <- struct{}{}
	}
}

func NewImageNameTranslator(source ConfigSource) *ImageNameTranslator {
	env := strings.ToUpper(os.Getenv("IMAGE_REGEXP_TRANSLATION"))
	return &ImageNameTranslator{
		configSource: source,
		translations: map[string]*ImageTranslation{},
		AllowRegexp:  env == "1" || env == "TRUE" || env == "YES",
	}
}
