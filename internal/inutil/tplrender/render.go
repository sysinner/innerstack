// Copyright 2015 Eryx <evorui аt gmail dοt com>, All rights reserved.
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

package tplrender

import (
	"bytes"
	"regexp"
	"text/template"
)

var (
	renderKeyFilterRoles = []string{
		"/", "__",
		"-", "_",
	}
	renderTplReg = regexp.MustCompile(`{{(.*?)}}`)
)

func renderTplFilter(tpl string) string {
	return string(renderTplReg.ReplaceAllFunc([]byte(tpl), func(bs []byte) []byte {
		for i := 0; i < len(renderKeyFilterRoles); i += 2 {
			bs = bytes.ReplaceAll(bs,
				[]byte(renderKeyFilterRoles[i]), []byte(renderKeyFilterRoles[i+1]))
		}
		return bs
	}))
}

func Render(src string, sets interface{}) ([]byte, error) {

	//
	tpl, err := template.New("s").Parse(renderTplFilter(src))
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, sets); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
