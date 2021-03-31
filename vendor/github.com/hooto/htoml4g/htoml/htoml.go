// Copyright 2019 Eryx <evorui аt gmаil dοt cοm>, All rights reserved.
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

package htoml

import (
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
	"os"

	"github.com/hooto/htoml4g/internal/toml" // fork from "github.com/BurntSushi/toml"
)

const (
	Version = "0.9.2"
)

type Key = toml.Key

type EncodeOptionIndent string

type EncodeOptionFilter = toml.EncodeOptionFilter

type EncodeOptions struct {
	Indent  string
	Filters []EncodeOptionFilter
}

func NewEncodeOptions() *EncodeOptions {
	return &EncodeOptions{Indent: "  "}
}

func Decode(obj interface{}, bs []byte) error {

	if _, err := toml.Decode(string(bs), obj); err != nil {
		return err
	}

	return nil
}

func DecodeFromFile(obj interface{}, file string) error {

	fp, err := os.Open(file)
	if err != nil {
		return err
	}
	defer fp.Close()

	bs, err := ioutil.ReadAll(fp)
	if err != nil {
		return err
	}

	if _, err := toml.Decode(string(bs), obj); err != nil {
		return err
	}

	return nil
}

func encodeOptionsParse(args ...interface{}) *EncodeOptions {
	opts := NewEncodeOptions()
	for _, arg := range args {
		switch arg.(type) {
		case EncodeOptions:
			o := arg.(EncodeOptions)
			opts = &o

		case *EncodeOptions:
			opts = arg.(*EncodeOptions)

		case EncodeOptionFilter:
			opts.Filters = append(opts.Filters, arg.(EncodeOptionFilter))

		case EncodeOptionIndent:
			opts.Indent = string(arg.(EncodeOptionIndent))
		}
	}
	return opts
}

func Encode(obj interface{}, args ...interface{}) ([]byte, error) {

	var buf bytes.Buffer
	if err := prettyEncode(obj, &buf, encodeOptionsParse(args...)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func EncodeToFile(obj interface{}, file string, args ...interface{}) error {

	fpo, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE, 0640)
	if err != nil {
		return err
	}
	defer fpo.Close()

	fpo.Seek(0, 0)
	fpo.Truncate(0)

	var wbuf = bufio.NewWriter(fpo)

	err = prettyEncode(obj, wbuf, encodeOptionsParse(args...))
	if err != nil {
		return err
	}

	return wbuf.Flush()
}

func prettyEncode(obj interface{}, bufOut io.Writer, opts *EncodeOptions) error {

	enc := toml.NewEncoder(bufOut)

	if opts != nil {
		enc.Indent = opts.Indent
		enc.Filters = opts.Filters
	}

	if err := enc.Encode(obj); err != nil {
		return err
	}

	return nil
}
