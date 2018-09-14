/*
Copyright 2018 Mirantis

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
	"fmt"
	"regexp"
	"strings"

	"github.com/Mirantis/virtlet/pkg/utils"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

type CmdSpec struct {
	Match  string
	Stdout string
}

type fakeCommand struct {
	rec       testutils.Recorder
	cmd       []string
	commander *FakeCommander
}

var _ utils.Command = &fakeCommand{}

func (c *fakeCommand) subst(text string) string {
	if c.commander.replacePath == nil {
		return text
	}
	return c.commander.replacePath.ReplaceAllString(text, c.commander.replacement)
}

func (c *fakeCommand) Run(stdin []byte) ([]byte, error) {
	fullCmd := c.subst(strings.Join(c.cmd, " "))
	r := map[string]string{
		"cmd": fullCmd,
	}
	defer c.rec.Rec("CMD", r)
	for _, spec := range c.commander.specs {
		matched, err := regexp.MatchString(spec.Match, fullCmd)
		if err != nil {
			return nil, fmt.Errorf("failed to match regexp %q: %v", spec.Match, err)
		}
		if matched {
			if c.rec != nil {
				if stdin != nil {
					r["stdin"] = c.subst(string(stdin))
				}
				if spec.Stdout != "" {
					r["stdout"] = spec.Stdout
				}
			}
			return []byte(spec.Stdout), nil
		}
	}
	return nil, fmt.Errorf("unexpected command %q", fullCmd)
}

// FakeCommander records the commands instead of executing them.  It
// also provides stdout based on a table of (cmd_regexp, stdout)
// pairs. The regexp is matched against the command and its arguments
// joined with " ".
type FakeCommander struct {
	rec         testutils.Recorder
	specs       []CmdSpec
	replacePath *regexp.Regexp
	replacement string
}

var _ utils.Commander = &FakeCommander{}

// NewFakeCommander creates a new FakeCommander.
// If rec is nil, all the commands will not be recorded
func NewFakeCommander(rec testutils.Recorder, specs []CmdSpec) *FakeCommander {
	return &FakeCommander{rec: rec, specs: specs}
}

// ReplaceTempPath makes the commander replace the path with specified
// suffix with the specified string. The replacement is done on the
// word boundary.
func (c *FakeCommander) ReplaceTempPath(pathSuffix, replacement string) {
	c.replacePath = regexp.MustCompile(`\S*` + regexp.QuoteMeta(pathSuffix))
	c.replacement = replacement
}

// Command implements the Command method of FakeCommander interface.
func (c *FakeCommander) Command(name string, arg ...string) utils.Command {
	return &fakeCommand{
		rec:       c.rec,
		cmd:       append([]string{name}, arg...),
		commander: c,
	}
}
