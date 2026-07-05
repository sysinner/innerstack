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

package cli

import (
	"github.com/sysinner/innerstack/v2/pkg/inconf"
)

var appConfigHelper inconf.AppReplicaHelper

func appSetup() (inconf.AppReplicaHelper, error) {
	if appConfigHelper == nil {
		if ap, err := inconf.NewAppReplicaHelper(); err == nil {
			appConfigHelper = ap
		} else {
			return nil, err
		}
	}
	return appConfigHelper, nil
}
