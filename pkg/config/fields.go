/*
Copyright 2018 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or ≈git-agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/golang/glog"
	"github.com/kballard/go-shellquote"
	flag "github.com/spf13/pflag"
	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
)

type configField interface {
	typeName() string
	fieldName() string
	flagName() string
	envName() string
	defaultStr() string
	applyDefault()
	clear()
	present() bool
	addFlag(f *flag.FlagSet)
	override(from configField)
	setFromEnvValue(value string)
	envValue() string
	schemaProps() (string, apiext.JSONSchemaProps)
	description() string
}

type fieldBase struct {
	field     string
	flag      string
	shorthand string
	desc      string
	env       string
}

func (f *fieldBase) fieldName() string   { return f.field }
func (f *fieldBase) flagName() string    { return f.flag }
func (f *fieldBase) envName() string     { return f.env }
func (f *fieldBase) description() string { return f.desc }

type stringField struct {
	fieldBase
	defValue string
	pattern  string
	value    **string
}

var _ configField = &stringField{}

func (sf *stringField) typeName() string   { return "string" }
func (sf *stringField) defaultStr() string { return sf.defValue }
func (sf *stringField) applyDefault() {
	if *sf.value == nil {
		*sf.value = &sf.defValue
	}
}

func (sf *stringField) clear()        { *sf.value = nil }
func (sf *stringField) present() bool { return *sf.value != nil }

func (sf *stringField) addFlag(f *flag.FlagSet) {
	f.StringVarP(*sf.value, sf.flag, sf.shorthand, sf.defValue, sf.desc)
}

func (sf *stringField) override(from configField) {
	fromValue := *from.(*stringField).value
	if fromValue != nil {
		v := *fromValue
		*sf.value = &v
	}
}

func (sf *stringField) setFromEnvValue(value string) {
	*sf.value = &value
}

func (sf *stringField) envValue() string {
	if *sf.value == nil {
		return ""
	}
	return **sf.value
}

func (sf *stringField) schemaProps() (string, apiext.JSONSchemaProps) {
	return sf.field, apiext.JSONSchemaProps{
		Type:    "string",
		Pattern: sf.pattern,
	}
}

type boolField struct {
	fieldBase
	defValue bool
	value    **bool
}

var _ configField = &boolField{}

func (bf *boolField) typeName() string   { return "boolean" }
func (bf *boolField) defaultStr() string { return strconv.FormatBool(bf.defValue) }
func (bf *boolField) applyDefault() {
	if *bf.value == nil {
		*bf.value = &bf.defValue
	}
}

func (bf *boolField) clear()        { *bf.value = nil }
func (bf *boolField) present() bool { return *bf.value != nil }

func (bf *boolField) addFlag(f *flag.FlagSet) {
	f.BoolVarP(*bf.value, bf.flag, bf.shorthand, bf.defValue, bf.desc)
}

func (bf *boolField) override(from configField) {
	fromValue := *from.(*boolField).value
	if fromValue != nil {
		v := *fromValue
		*bf.value = &v
	}
}

func (bf *boolField) setFromEnvValue(value string) {
	v := value != ""
	*bf.value = &v
}

func (bf *boolField) envValue() string {
	if *bf.value == nil || !**bf.value {
		return ""
	}
	return "1"
}

func (bf *boolField) schemaProps() (string, apiext.JSONSchemaProps) {
	return bf.field, apiext.JSONSchemaProps{
		Type: "boolean",
	}
}

type intField struct {
	fieldBase
	defValue int
	min      int
	max      int
	value    **int
}

var _ configField = &intField{}

func (intf *intField) typeName() string   { return "integer" }
func (intf *intField) defaultStr() string { return strconv.Itoa(intf.defValue) }
func (intf *intField) applyDefault() {
	if *intf.value == nil {
		*intf.value = &intf.defValue
	}
}

func (intf *intField) clear()        { *intf.value = nil }
func (intf *intField) present() bool { return *intf.value != nil }

func (intf *intField) addFlag(f *flag.FlagSet) {
	f.IntVarP(*intf.value, intf.flag, intf.shorthand, intf.defValue, intf.desc)
}

func (intf *intField) override(from configField) {
	fromValue := *from.(*intField).value
	if fromValue != nil {
		v := *fromValue
		*intf.value = &v
	}
}

func (intf *intField) setFromEnvValue(value string) {
	if v, err := strconv.Atoi(value); err != nil {
		glog.Warningf("bad value for int field %s: %q", intf.field, value)
	} else {
		*intf.value = &v
	}
}

func (intf *intField) envValue() string {
	if *intf.value == nil {
		return ""
	}
	return strconv.Itoa(**intf.value)
}

func (intf *intField) schemaProps() (string, apiext.JSONSchemaProps) {
	min := float64(intf.min)
	max := float64(intf.max)
	return intf.field, apiext.JSONSchemaProps{
		Type:    "integer",
		Minimum: &min,
		Maximum: &max,
	}
}

type envLookup func(name string) (string, bool)

type fieldSet struct {
	fields   []configField
	docTitle string
	desc     string
}

func (fs *fieldSet) addStringFieldWithPattern(field, flag, shorthand, desc, env, defValue, pattern string, value **string) {
	fs.addField(&stringField{
		fieldBase{field, flag, shorthand, desc, env},
		defValue,
		pattern,
		value,
	})
}

func (fs *fieldSet) addStringField(field, flag, shorthand, desc, env, defValue string, value **string) {
	fs.addStringFieldWithPattern(field, flag, shorthand, desc, env, defValue, "", value)
}

func (fs *fieldSet) addBoolField(field, flag, shorthand, desc, env string, defValue bool, value **bool) {
	fs.addField(&boolField{
		fieldBase{field, flag, shorthand, desc, env},
		defValue,
		value,
	})
}

func (fs *fieldSet) addIntField(field, flag, shorthand, desc, env string, defValue, min, max int, value **int) {
	fs.addField(&intField{
		fieldBase{field, flag, shorthand, desc, env},
		defValue,
		min,
		max,
		value,
	})
}

func (fs *fieldSet) addField(cf configField) {
	fs.fields = append(fs.fields, cf)
}

func (fs *fieldSet) applyDefaults() {
	for _, f := range fs.fields {
		f.applyDefault()
	}
}

func (fs *fieldSet) addFlags(flagSet *flag.FlagSet) {
	for _, f := range fs.fields {
		if f.flagName() != "" && !strings.Contains(f.flagName(), "+") {
			f.addFlag(flagSet)
		}
	}
}

func (fs *fieldSet) override(from *fieldSet) {
	for n, f := range fs.fields {
		f.override(from.fields[n])
	}
}

func (fs *fieldSet) copyFrom(from *fieldSet) {
	for n, f := range fs.fields {
		f.clear()
		f.override(from.fields[n])
	}
}

func (fs *fieldSet) clearFieldsNotInFlagSet(flagSet *flag.FlagSet) {
	for _, f := range fs.fields {
		if flagSet == nil || f.flagName() == "" || !flagSet.Changed(f.flagName()) {
			f.clear()
		}
	}
}

func (fs *fieldSet) setFromEnv(lookupEnv envLookup) {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	for _, f := range fs.fields {
		if f.envName() == "" {
			continue
		}
		if v, found := lookupEnv(f.envName()); found {
			f.setFromEnvValue(v)
		}
	}
}

func (fs *fieldSet) dumpEnv() string {
	var buf bytes.Buffer
	for _, f := range fs.fields {
		if f.envName() != "" && f.present() {
			fmt.Fprintf(&buf, "export %s=%s\n", f.envName(), shellquote.Join(f.envValue()))
		}
	}
	return buf.String()
}

func (fs *fieldSet) schemaProps() map[string]apiext.JSONSchemaProps {
	r := make(map[string]apiext.JSONSchemaProps)
	for _, f := range fs.fields {
		field, props := f.schemaProps()
		r[field] = props
	}
	return r
}

func (fs *fieldSet) generateDoc() string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf,
		"| Description | Config field | Default value | Type | Command line flag / Env |\n"+
			"| --- | --- | --- | --- | --- |\n")
	esc := func(s string) string { return strings.Replace(s, "|", "\\|", -1) }
	code := func(s string) string {
		if s == "" {
			return ""
		}
		return fmt.Sprintf("`%s`", esc(s))
	}
	for _, f := range fs.fields {
		if f.description() == "" {
			continue
		}
		var flagEnv []string
		if f.flagName() != "" {
			flagEnv = append(flagEnv, code("--"+strings.Replace(f.flagName(), "+", "", -1)))
		}
		if f.envName() != "" {
			flagEnv = append(flagEnv, code(f.envName()))
		}
		fmt.Fprintf(&buf, "| %s | %s | %s | %s | %s |\n",
			esc(f.description()),
			code(f.fieldName()),
			code(f.defaultStr()),
			f.typeName(),
			strings.Join(flagEnv, " / "))
	}
	return buf.String()
}
