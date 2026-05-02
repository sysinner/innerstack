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

import "testing"

func TestDNSNameValid(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// valid cases
		{"valid simple name", "myapp", false},
		{"valid with hyphen", "my-app", false},
		{"valid starts with digit", "3rd-app", false},
		{"valid starts with letter digit hyphen mix", "app1-prod", false},
		{"valid min length 3", "abc", false},
		{"valid max length 63", "a23456789012345678901234567890123456789012345678901234567890123", false},
		{"valid with multiple hyphens", "my-app-prod-01", false},
		{"valid single segment with digits", "node123", false},
		{"valid all digits", "123", false},

		// invalid cases: required
		{"empty string", "", true},

		// invalid cases: min=3
		{"too short 1 char", "a", true},
		{"too short 2 chars", "ab", true},

		// invalid cases: max=63
		{"too long 64 chars", "a234567890123456789012345678901234567890123456789012345678901234", true},

		// invalid cases: rfc1123_compliant
		{"starts with hyphen", "-app", true},
		{"ends with hyphen", "app-", true},
		{"contains underscore", "my_app", true},
		{"contains space", "my app", true},
		{"contains dot", "my.app", true},
		{"contains special char", "my@app", true},
		{"contains uppercase", "MyApp", true},
		{"only hyphens", "---", true},
		{"starts with hyphen and valid rest", "-myapp", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DNSLabelValid(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("DNSLabelValid(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
