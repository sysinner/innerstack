// Copyright 2020 Eryx <evorui аt gmail dοt com>, All rights reserved.
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

package hmsg

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"regexp"
	"text/template"
)

func uint32ToBytes(v uint32) []byte {
	bs := make([]byte, 4)
	binary.BigEndian.PutUint32(bs, v)
	return bs
}

func bytesToHexString(bs []byte) string {
	return hex.EncodeToString(bs)
}

func uint32ToHexString(v uint32) string {
	return bytesToHexString(uint32ToBytes(v))
}

var (
	txtRenderKeyFilterRoles = []string{
		"/", "__",
		"-", "__",
	}
	txtRenderTplRE = regexp.MustCompile(`{{(.*?)}}`)
)

func txtRenderTplFilter(tpl string) string {
	return string(txtRenderTplRE.ReplaceAllFunc([]byte(tpl), func(bs []byte) []byte {
		for i := 0; i < len(txtRenderKeyFilterRoles); i += 2 {
			bs = bytes.ReplaceAll(bs,
				[]byte(txtRenderKeyFilterRoles[i]), []byte(txtRenderKeyFilterRoles[i+1]))
		}
		return bs
	}))
}

func txtRender(src string, sets interface{}) (string, error) {

	tpl, err := template.New("s").Parse(txtRenderTplFilter(src))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, sets); err != nil {
		return "", err
	}

	return buf.String(), nil
}
