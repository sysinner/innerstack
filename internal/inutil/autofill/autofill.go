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

// Package autofill provides auto-fill value generation for AppSpecConfigItem
// auto-fill expressions. Supported types: defval, hexstr_32, hexstr_16, uuid.
package autofill

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/sysinner/innerstack/v2/internal/inutil"
)

const (
	// DefVal uses the field's default value (returns empty, caller should use field.Default).
	DefVal = "defval"
	// HexStr32 generates a 32-character hex string (16 random bytes).
	HexStr32 = "hexstr_32"
	// HexStr16 generates a 16-character hex string (8 random bytes).
	HexStr16 = "hexstr_16"
	// UUID generates a UUID v4 string.
	UUID = "uuid"
)

// Generate produces a value based on the auto-fill type.
// For DefVal, returns empty string — the caller should fall back to the field's Default.
func Generate(autoFill string) (string, error) {
	switch autoFill {
	case DefVal:
		return "", nil
	case HexStr32:
		return inutil.RandHexString(16), nil
	case HexStr16:
		return inutil.RandHexString(8), nil
	case UUID:
		return uuid.New().String(), nil
	default:
		return "", nil
	}
}

// GenerateIfEmpty generates an auto-fill value only if the current value is empty.
// Returns the current value if non-empty, otherwise generates a new value.
func GenerateIfEmpty(autoFill, currentValue string) (string, error) {
	if currentValue != "" {
		return currentValue, nil
	}
	if autoFill == "" {
		return "", nil
	}
	val, err := Generate(autoFill)
	if err != nil {
		return "", fmt.Errorf("[autofill.GenerateIfEmpty] %w", err)
	}
	return val, nil
}
