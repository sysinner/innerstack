// Copyright 2018 Eryx <evorui аt gmail dοt com>, All rights reserved.
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

package hini

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/lessos/lessgo/types"
)

const (
	Version = "0.1.0"
)

var (
	sectionStart = []byte{'['}
	sectionClose = []byte{']'}
)

type Options struct {
	mu     sync.Mutex
	parsed bool
	file   string
	str    string
	data   types.Labels
	vars   map[string]string
}

func ParseFile(file string) (*Options, error) {

	fp, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer fp.Close()

	return &Options{
		file: file,
		vars: map[string]string{},
	}, nil
}

func ParseString(str string) (*Options, error) {
	return &Options{
		str:  str,
		vars: map[string]string{},
	}, nil
}

func (it *Options) parse() {

	it.mu.Lock()
	defer it.mu.Unlock()

	if it.parsed {
		return
	}

	var (
		buf *bufio.Reader
		err error
	)

	if it.str != "" {
		buf = bufio.NewReader(strings.NewReader(it.str))
	} else if it.file != "" {

		fp, err := os.Open(it.file)
		if err != nil {
			return
		}
		defer fp.Close()

		buf = bufio.NewReader(fp)
	} else {
		return
	}

	bom, err := buf.Peek(3)
	if err == nil && bom[0] == 239 && bom[1] == 187 && bom[2] == 191 {
		for i := 1; i <= 3; i++ {
			buf.ReadByte()
		}
	}

	var (
		section_open = "main"
		tag_open     = ""
		tag_value    = ""
	)

	for {

		line, _, err := buf.ReadLine()
		if err != nil {
			break
		}

		line = bytes.TrimRight(line, " ")
		if len(line) < 1 {
			continue
		}

		if bytes.HasPrefix(line, sectionStart) && bytes.HasSuffix(line, sectionClose) {

			section := strings.TrimSpace(strings.ToLower(string(line[1 : len(line)-1])))

			section = strings.Replace(section, "\"", "", -1)
			section = strings.Replace(section, " ", "/", -1)

			if section != section_open {

				if tag_open != "" {
					it.data.Set(section_open+"/"+tag_open, tag_value)
					tag_open, tag_value = "", ""
				}
			}

			section_open = section
		}

		if tag_open == "" && bytes.HasPrefix(line, []byte{'#'}) {
			continue
		}

		if bytes.HasPrefix(line, []byte{'%'}) {

			section_open = "main"

			if tag_open != "" {
				it.data.Set(section_open+"/"+tag_open, tag_value)
			}

			if len(line) > 1 {
				tag_open = strings.ToLower(string(line[1:]))
			} else {
				tag_open = ""
			}

			tag_value = ""

			continue
		}

		if tag_open != "" {

			if len(tag_value) > 0 {
				tag_value += "\n"
			}

			tag_value += string(line)

			continue
		}

		if kv := bytes.Split(line, []byte{'='}); len(kv) == 2 {

			key := strings.ToLower(string(bytes.TrimSpace(kv[0])))
			val := string(bytes.TrimSpace(kv[1]))

			if len(key) > 0 && len(val) > 0 {
				it.data.Set(section_open+"/"+key, val)
			}
		}
	}

	if tag_open != "" {
		it.data.Set(section_open+"/"+tag_open, tag_value)
	}

	it.parsed = true
}

func (it *Options) VarSet(args ...string) {

	if len(args) < 2 || len(args)%2 != 0 {
		return
	}

	for i := 0; i < len(args); i += 2 {
		it.vars[args[i]] = args[i+1]
	}
}

func (it *Options) valueRender(val string) string {

	for k, v := range it.vars {
		val = strings.Replace(val, "{{."+k+"}}", v, -1)
	}

	return val
}

func (it *Options) ValueOK(args ...string) (types.Bytex, bool) {

	it.parse()

	var (
		val types.Bytex
		ok  = false
	)

	if len(args) == 1 {
		if strings.IndexByte(args[0], '/') > 0 {
			val, ok = it.data.Get(args[0])
		} else {
			val, ok = it.data.Get("main/" + args[0])
		}
	} else if len(args) == 2 {
		val, ok = it.data.Get(args[0] + "/" + args[1])
	}

	if len(val) > 0 {
		val = types.Bytex([]byte(it.valueRender(val.String())))
	}

	return val, ok
}

func (it *Options) Value(args ...string) types.Bytex {
	v, _ := it.ValueOK(args...)
	return v
}

func (it *Options) Set(args ...string) error {

	if len(args) < 2 {
		return errors.New("Invalid Args")
	}

	it.parse()

	if len(args) == 3 {
		it.data.Set(args[0]+"/"+args[1], args[2])
	} else if strings.IndexByte(args[0], '/') > 0 {
		it.data.Set(args[0], args[1])
	} else {
		it.data.Set("main/"+args[0], args[1])
	}

	return nil
}
