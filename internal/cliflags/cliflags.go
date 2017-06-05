// Copyright 2016 lessos Authors, All rights reserved.
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

package cliflags // import "code.hooto.com/lessos/los-soho/internal/cliflags"

import (
	"os"

	"github.com/lessos/lessgo/types"
)

var (
	args_kv = map[string]types.Bytex{}
)

func init() {

	if len(os.Args) < 2 {
		return
	}

	for i, k := range os.Args {

		if k[0] != '-' || len(k) < 2 {
			continue
		}

		v := ""

		if len(os.Args) <= i+1 {
			args_kv[k[1:]] = types.Bytex([]byte(""))
			continue
		}

		v = os.Args[i+1]

		args_kv[k[1:]] = types.Bytex([]byte(v))
	}
}

func Value(key string) (types.Bytex, bool) {

	if v, ok := args_kv[key]; ok {
		return v, ok
	}

	return nil, false
}
