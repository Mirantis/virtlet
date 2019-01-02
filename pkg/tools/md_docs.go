// This file is based on the code from https://github.com/spf13/cobra
// Original copyright follows
//Copyright 2015 Red Hat Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tools

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func printOptions(out io.Writer, cmd *cobra.Command, name string) error {
	if cmd.HasParent() {
		return printFlagUsages(out, cmd.NonInheritedFlags(), "\n**Options**\n\n", false)
	}

	return printFlagUsages(out, cmd.NonInheritedFlags(), "\n## Global options\n\n", true)
}

func printSubcommands(out io.Writer, cmd *cobra.Command) error {
	children := cmd.Commands()
	sort.Sort(byName(children))

	var filtered []*cobra.Command

	for _, c := range children {
		if c.IsAvailableCommand() && !c.IsAdditionalHelpTopicCommand() {
			filtered = append(filtered, c)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(out, "\n**Subcommands**\n\n"); err != nil {
		return err
	}

	name := cmd.CommandPath()
	for _, c := range filtered {
		cname := name + " " + c.Name()
		// FIXME: use better link name generation scheme
		link := "#" + strings.Replace(cname, " ", "-", -1)
		fmt.Fprintf(out, "* [%s](%s) - %s\n", cname, link, c.Short)

	}

	for _, c := range filtered {
		if err := GenMarkdown(c, out); err != nil {
			return err
		}
	}

	return nil
}

// GenMarkdown creates markdown output.
func GenMarkdown(cmd *cobra.Command, w io.Writer) error {
	cmd.InitDefaultHelpCmd()
	cmd.InitDefaultHelpFlag()

	name := cmd.CommandPath()

	short := cmd.Short
	long := cmd.Long
	if len(long) == 0 {
		long = short
	}

	_, err := fmt.Fprintf(w, "## %s\n\n", name)
	if err == nil {
		_, err = fmt.Fprintf(w, "%s\n\n", short)
	}
	if err == nil {
		_, err = fmt.Fprintf(w, "**Synopsis**\n\n%s\n\n", long)
	}

	if err == nil && cmd.Runnable() {
		_, err = fmt.Fprintf(w, "```\n%s\n```\n\n", cmd.UseLine())
	}

	if err == nil && len(cmd.Example) > 0 {
		_, err = fmt.Fprintf(w, "**Examples**\n\n```\n%s\n```\n\n", cmd.Example)
	}

	// we want global options to be at the end of the document
	if err == nil && cmd.HasParent() {
		err = printOptions(w, cmd, name)
	}

	if err == nil {
		err = printSubcommands(w, cmd)
	}

	if err == nil && !cmd.HasParent() {
		err = printOptions(w, cmd, name)
	}

	return err
}

// GenMarkdownTree will generate a markdown page for this command and all
// descendants in the directory given. The header may be nil.
// This function may not work correctly if your command names have `-` in them.
// If you have `cmd` with two subcmds, `sub` and `sub-third`,
// and `sub` has a subcommand called `third`, it is undefined which
// help output will be in the file `cmd-sub-third.1`.
func GenMarkdownTree(cmd *cobra.Command, dir string) error {
	return GenMarkdownTreeCustom(cmd, dir)
}

// GenMarkdownTreeCustom is the the same as GenMarkdownTree, but
// with custom filePrepender and linkHandler.
func GenMarkdownTreeCustom(cmd *cobra.Command, dir string) error {
	basename := strings.Replace(cmd.CommandPath(), " ", "_", -1) + ".md"
	filename := filepath.Join(dir, basename)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return GenMarkdown(cmd, f)
}

type byName []*cobra.Command

func (s byName) Len() int           { return len(s) }
func (s byName) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byName) Less(i, j int) bool { return s[i].Name() < s[j].Name() }

func printFlagUsages(out io.Writer, f *pflag.FlagSet, title string, includeHelpFlag bool) error {
	titlePrinted := false
	var err error
	f.VisitAll(func(flag *pflag.Flag) {
		if err != nil {
			return
		}

		if flag.Deprecated != "" || flag.Hidden {
			return
		}

		if !includeHelpFlag && flag.Name == "help" {
			return
		}

		if !titlePrinted {
			if _, err = out.Write([]byte(title)); err != nil {
				return
			}
			titlePrinted = true
		}

		varStr := ""
		varname, usage := pflag.UnquoteUsage(flag)
		if varname != "" {
			varStr = " " + varname
		}
		if flag.NoOptDefVal != "" {
			switch flag.Value.Type() {
			case "string":
				varStr += fmt.Sprintf("[=\"%s\"]", flag.NoOptDefVal)
			case "bool":
				if flag.NoOptDefVal != "true" {
					varStr += fmt.Sprintf("[=%s]", flag.NoOptDefVal)
				}
			case "count":
				if flag.NoOptDefVal != "+1" {
					varStr += fmt.Sprintf("[=%s]", flag.NoOptDefVal)
				}
			default:
				varStr += fmt.Sprintf("[=%s]", flag.NoOptDefVal)
			}
		}

		if flag.Shorthand != "" && flag.ShorthandDeprecated == "" {
			_, err = fmt.Fprintf(out, "\n```\n-%s, --%s%s\n```\n", flag.Shorthand, flag.Name, varStr)
		} else {
			_, err = fmt.Fprintf(out, "\n```\n--%s%s\n```\n", flag.Name, varStr)
		}

		if err == nil {
			_, err = fmt.Fprintf(out, "%s\n", usage)
		}

		if err == nil && !defaultIsZeroValue(flag) {
			if flag.Value.Type() == "string" {
				_, err = fmt.Fprintf(out, " **(default value:** `%q`)\n", flag.DefValue)
			} else {
				_, err = fmt.Fprintf(out, " **(default value:** `%s`)\n", flag.DefValue)
			}
		}
	})

	return err
}

// defaultIsZeroValue returns true if the default value for this flag represents
// a zero value.
func defaultIsZeroValue(f *pflag.Flag) bool {
	switch f.Value.Type() {
	case "bool":
		return f.DefValue == "false"
	case "duration":
		// Beginning in Go 1.7, duration zero values are "0s"
		return f.DefValue == "0" || f.DefValue == "0s"
	case "int", "int8", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "count", "float32", "float64":
		return f.DefValue == "0"
	case "string":
		return f.DefValue == ""
	case "ip", "ipMask", "ipNet":
		return f.DefValue == "<nil>"
	case "intSlice", "stringSlice", "stringArray":
		return f.DefValue == "[]"
	default:
		switch f.Value.String() {
		case "false":
			return true
		case "<nil>":
			return true
		case "":
			return true
		case "0":
			return true
		}
		return false
	}
}
