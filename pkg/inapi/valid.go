// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package inapi

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/go-playground/locales/en"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	en_translations "github.com/go-playground/validator/v10/translations/en"
	"golang.org/x/mod/semver"
)

// dnsLabelRegexp matches a single RFC 1123 DNS label:
// - lowercase letters, digits, and hyphens only
// - must not start or end with a hyphen
var dnsLabelRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

type Validator func(string) error

var (
	NameValid Validator

	// RFC 1123 DNS label
	DNSLabelValid Validator

	Ip4AddrValid Validator

	ObjectIdValid = regexp.MustCompile("^[0-9a-f]{12,16}$")

	validate = validator.New()

	trans ut.Translator
)

func init() {

	var (
		en  = en.New()
		uni = ut.New(en, en)
	)

	trans, _ = uni.GetTranslator("en")

	en_translations.RegisterDefaultTranslations(validate, trans)

	// Register custom RFC 1123 label validator
	validate.RegisterValidation("dns_label", func(fl validator.FieldLevel) bool {
		return dnsLabelRegexp.MatchString(fl.Field().String())
	})

	//
	NameValid = newValidator("required,min=3,max=20,alphanum")

	// RFC 1123
	DNSLabelValid = newValidator("required,dns_label,min=3,max=63")

	//
	Ip4AddrValid = newValidator("required,tcp4_addr")
}

func newValidator(rule string) Validator {
	return func(str string) error {
		if err := validate.Var(str, rule); err != nil {
			errs := err.(validator.ValidationErrors)
			return errors.New(errs[0].Translate(trans))
		}
		return nil
	}
}

// SemverValid checks whether the given version string is a valid semantic version.
// The version must follow the full SemVer 2.0 format: MAJOR.MINOR.PATCH with
// optional pre-release (-alpha.1) and build (+build.123) tags.
// Note: golang.org/x/mod/semver requires a "v" prefix for canonical comparison,
// so we prepend "v" before validation.
func SemverValid(version string) error {
	if version == "" {
		return errors.New("version is required")
	}

	v := "v" + version
	if !semver.IsValid(v) {
		return fmt.Errorf("invalid semver format: %q (expected MAJOR.MINOR.PATCH)", version)
	}

	// golang.org/x/mod/semver treats "v1" and "v1.0" as valid, but we require
	// the strict MAJOR.MINOR.PATCH format (core must contain exactly 2 dots).
	core := version
	for i := 0; i < len(core); i++ {
		if core[i] == '-' || core[i] == '+' {
			core = core[:i]
			break
		}
	}
	dotCount := 0
	for _, c := range core {
		if c == '.' {
			dotCount++
		}
	}
	if dotCount != 2 {
		return fmt.Errorf("invalid semver format: %q (expected MAJOR.MINOR.PATCH)", version)
	}

	return nil
}

// ValidateTaskTrigger validates that exactly one trigger field is set in a task.
// Trigger fields are mutually exclusive: on_startup, on_shutdown, interval_seconds, cron.
// Each task must have exactly one trigger field set.
func ValidateTaskTrigger(task *AppSpecTask) error {
	if task == nil {
		return errors.New("task is nil")
	}

	triggers := 0

	if task.OnStartup {
		triggers++
	}
	if task.OnShutdown {
		triggers++
	}
	if task.IntervalSeconds > 0 {
		triggers++
	}
	if task.Cron != "" {
		triggers++
	}

	if triggers == 0 {
		return errors.New("exactly one trigger field is required (on_startup, on_shutdown, interval_seconds, or cron)")
	}

	if triggers > 1 {
		return errors.New("trigger fields are mutually exclusive (on_startup, on_shutdown, interval_seconds, cron), only one can be set")
	}

	return nil
}

// ValidateSpecConfigField validates the schema of a single AppSpecConfigItem.
// It checks that the field and each of its items has a non-empty, sibling-
// unique name, and that "group" / "array_group" types carry the required
// structure (at least one item; for array_group a key_item naming one item).
func ValidateSpecConfigField(field *AppSpecConfigItem) error {
	if field == nil {
		return errors.New("config field is nil")
	}
	if field.Name == "" {
		return errors.New("config field name is required")
	}

	// Validate child items: names non-empty, unique among siblings, recursively
	// well-formed.
	if len(field.Items) > 0 {
		names := make(map[string]struct{}, len(field.Items))
		for _, sub := range field.Items {
			if sub == nil {
				continue
			}
			if sub.Name == "" {
				return fmt.Errorf("config field %q: sub-item name is required", field.Name)
			}
			if _, ok := names[sub.Name]; ok {
				return fmt.Errorf("config field %q: duplicate sub-item name %q", field.Name, sub.Name)
			}
			names[sub.Name] = struct{}{}
			if err := ValidateSpecConfigField(sub); err != nil {
				return err
			}
		}
	}

	// Validate the unique scope (if declared).
	if field.Unique != "" &&
		field.Unique != SpecConfigUniqueArrayGroup &&
		field.Unique != SpecConfigUniqueApp {
		return fmt.Errorf("config field %q: invalid unique scope %q (want %q or %q)",
			field.Name, field.Unique, SpecConfigUniqueArrayGroup, SpecConfigUniqueApp)
	}

	switch field.Type {
	case SpecFieldTypeGroup:
		if len(field.Items) == 0 {
			return fmt.Errorf("config field %q: group requires at least one item", field.Name)
		}
	case SpecFieldTypeArrayGroup:
		if len(field.Items) == 0 {
			return fmt.Errorf("config field %q: array_group requires at least one item", field.Name)
		}
		if field.KeyItem == "" {
			return fmt.Errorf("config field %q: array_group requires key_item", field.Name)
		}
		if !specConfigItemContainsName(field.Items, field.KeyItem) {
			return fmt.Errorf(
				"config field %q: array_group key_item %q does not match any item name",
				field.Name, field.KeyItem)
		}
	}

	return nil
}

// specConfigItemContainsName reports whether any non-nil item has the given name.
func specConfigItemContainsName(items []*AppSpecConfigItem, name string) bool {
	for _, it := range items {
		if it != nil && it.Name == name {
			return true
		}
	}
	return false
}

// ValidateDeployConfigItems validates deploy-time config values against their
// spec schema:
//
//   - For each "array_group" spec field: every instance carries a non-empty key
//     (the key_item value) and keys are unique within the array_group. Any child
//     field declared unique="array_group" is likewise distinct across instances.
//   - For any field declared unique="app": its values are pooled by field name
//     across the entire deploy tree (flat fields + every array_group instance)
//     and must be mutually distinct.
//
// Spec fields without a matching deploy item are skipped (a deploy may omit
// optional configs).
func ValidateDeployConfigItems(
	spec []*AppSpecConfigItem, deploy []*AppDeployConfigItem,
) error {
	if len(spec) == 0 || len(deploy) == 0 {
		return nil
	}

	specByName := make(map[string]*AppSpecConfigItem, len(spec))
	for _, sf := range spec {
		if sf != nil && sf.Name != "" {
			specByName[sf.Name] = sf
		}
	}

	// 1. array_group-scope uniqueness (key_item + unique="array_group" children)
	for _, sf := range spec {
		if sf == nil || sf.Type != SpecFieldTypeArrayGroup {
			continue
		}
		di := deployConfigItem(deploy, sf.Name)
		if di == nil {
			continue
		}

		// key_item: required non-empty and unique.
		if sf.KeyItem != "" {
			if err := checkArrayGroupUnique(di, sf.KeyItem, true); err != nil {
				return err
			}
		}
		// additional array_group-unique child fields.
		for _, child := range sf.Items {
			if child == nil || child.Name == "" ||
				child.Unique != SpecConfigUniqueArrayGroup ||
				child.Name == sf.KeyItem {
				continue
			}
			if err := checkArrayGroupUnique(di, child.Name, false); err != nil {
				return err
			}
		}
	}

	// 2. app-scope uniqueness (pool by field name across the whole deploy tree)
	appUniqueNames := map[string]struct{}{}
	collectAppUniqueNames(spec, appUniqueNames)
	if len(appUniqueNames) == 0 {
		return nil
	}

	pool := map[string][]string{}
	for _, di := range deploy {
		if di == nil {
			continue
		}
		collectDeployValues(di, specByName[di.Name], pool, appUniqueNames)
	}
	for name, vals := range pool {
		if err := checkDistinctAppScoped(name, vals); err != nil {
			return err
		}
	}

	return nil
}

// deployConfigItem returns the deploy item with the given name, or nil.
func deployConfigItem(items []*AppDeployConfigItem, name string) *AppDeployConfigItem {
	for _, it := range items {
		if it != nil && it.Name == name {
			return it
		}
	}
	return nil
}

// checkArrayGroupUnique scans the instances of an array_group deploy item for
// duplicate values of fieldName. When requireNonEmpty is true (used for
// key_item), an empty value is itself an error.
func checkArrayGroupUnique(di *AppDeployConfigItem, fieldName string,
	requireNonEmpty bool) error {

	seen := map[string]struct{}{}
	for i, inst := range di.Items {
		if inst == nil {
			continue
		}
		v := ""
		if it := inst.Item(fieldName); it != nil {
			v = it.Value
		}
		if v == "" {
			if requireNonEmpty {
				return fmt.Errorf(
					"config %q: array_group instance #%d missing key field %q",
					di.Name, i+1, fieldName)
			}
			continue
		}
		if _, ok := seen[v]; ok {
			return fmt.Errorf("config %q: array_group duplicate %s %q",
				di.Name, fieldName, v)
		}
		seen[v] = struct{}{}
	}
	return nil
}

// collectAppUniqueNames gathers the set of field names declared unique="app"
// anywhere in the spec tree (top-level fields and group/array_group child
// definitions). Uniqueness for such a name then applies to every value of that
// field across the deploy tree.
func collectAppUniqueNames(spec []*AppSpecConfigItem, out map[string]struct{}) {
	for _, sf := range spec {
		if sf == nil {
			continue
		}
		if sf.Unique == SpecConfigUniqueApp && sf.Name != "" {
			out[sf.Name] = struct{}{}
		}
		if len(sf.Items) > 0 {
			collectAppUniqueNames(sf.Items, out)
		}
	}
}

// collectDeployValues walks a top-level deploy item and appends, into pool, the
// values of fields whose name is in appUniqueNames. The deploy item structure
// (flat / group / array_group) is interpreted via the matching spec field.
func collectDeployValues(di *AppDeployConfigItem, sf *AppSpecConfigItem,
	pool map[string][]string, appUniqueNames map[string]struct{}) {

	want := func(name string) bool {
		_, ok := appUniqueNames[name]
		return ok
	}
	record := func(name, value string) {
		if name != "" && want(name) {
			pool[name] = append(pool[name], value)
		}
	}

	switch {
	case sf != nil && sf.Type == SpecFieldTypeArrayGroup:
		for _, inst := range di.Items {
			if inst == nil {
				continue
			}
			for _, f := range inst.Items {
				if f != nil {
					record(f.Name, f.Value)
				}
			}
		}
	case sf != nil && sf.Type == SpecFieldTypeGroup:
		for _, f := range di.Items {
			if f != nil {
				record(f.Name, f.Value)
			}
		}
	default: // flat
		record(di.Name, di.Value)
	}
}

// checkDistinctAppScoped reports an error if the (non-empty) values collected
// for an app-scoped unique field contain any duplicate.
func checkDistinctAppScoped(name string, vals []string) error {
	seen := map[string]struct{}{}
	for _, v := range vals {
		if v == "" {
			continue // unset values are not duplicates of each other
		}
		if _, ok := seen[v]; ok {
			return fmt.Errorf(
				"config %q: value %q must be unique (app scope) but is duplicated",
				name, v)
		}
		seen[v] = struct{}{}
	}
	return nil
}
