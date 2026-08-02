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

package pkgbuild

import "testing"

// TestValidArchConstants guards the contract that every exported arch constant
// is a accepted release arch, so callers (e.g. hostlet resolvePackage's src
// fallback) can rely on ArchSrc being a real, valid value.
func TestValidArchConstants(t *testing.T) {
	for _, a := range []string{ArchAMD64, ArchARM64, ArchSrc} {
		if !ValidArch[a] {
			t.Errorf("ValidArch[%q] = false, want true", a)
		}
	}
}
