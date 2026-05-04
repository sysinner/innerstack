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
