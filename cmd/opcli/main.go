// Copyright 2015 Eryx <evorui аt gmаil dοt cοm>, All rights reserved.
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

package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lessos/lessgo/types"
	"github.com/lynkdb/iomix/connect"
	"github.com/lynkdb/iomix/skv"
	"github.com/lynkdb/kvgo"

	"github.com/sysinner/innerstack/internal/cliflags"
)

var (
	dbcn skv.Connector
	err  error
)

func main() {

	if v, ok := cliflags.Value("db_export"); ok {
		db_export(v.String())
	} else if v, ok := cliflags.Value("db_import"); ok {
		db_import(v.String())
	}
}

func db_init() error {

	db_dir := ""

	if v, ok := cliflags.Value("db_dir"); ok {
		db_dir = strings.Trim(filepath.Clean(v.String()), ".")
	}

	if db_dir == "" {
		return errors.New("No Data Dir Found")
	}

	//
	opts := &connect.ConnOptions{
		Name:      "db_conn",
		Connector: "iomix/skv/Connector",
		Driver:    types.NewNameIdentifier("lynkdb/kvgo"),
	}
	opts.SetValue("data_dir", db_dir)

	if dbcn, err = kvgo.Open(*opts); err != nil {
		return fmt.Errorf("Can Not Connect To %s, Error: %s", opts.Name, err.Error())
	}

	return nil
}

func db_import(import_dir string) {

	//
	if len(import_dir) < 1 {
		log.Fatal("No Export DIR Found")
	}

	import_dir = filepath.Clean(import_dir)
	if _, err := os.Open(import_dir); err != nil {
		log.Fatal("No Import DIR Found")
	}

	fmt.Println("Import from", import_dir)

	if err := db_init(); err != nil {
		log.Fatal(err.Error())
	}
	defer dbcn.Close()

	filepath.Walk(import_dir, func(path string, info os.FileInfo, err error) error {

		if info.IsDir() {
			return nil
		}

		fp, err := os.Open(path)
		if err != nil {
			return err
		}
		defer fp.Close()

		bs, err := ioutil.ReadAll(fp)
		if err != nil {
			return err
		}

		var (
			dir           = filepath.Dir(path)[len(import_dir)+1:]
			name          = filepath.Base(path)
			value_type, _ = strconv.Atoi(name[:3])
			key           = []byte{12, uint8(len(dir))}
		)

		namebs, err := hex.DecodeString(name[4:])
		if err != nil {
			return err
		}
		key = append(append(key, []byte(dir)...), namebs...)

		if rs := dbcn.RawPut(key, append([]byte{uint8(value_type)}, bs...), 0); !rs.OK() {
			return errors.New("db put err " + rs.Bytex().String())
		}

		fmt.Println("  ", len(dir), dir, value_type, len(bs), "  ", name)
		return nil
	})
}

func db_export(export_dir string) {

	//
	if len(export_dir) < 1 {
		log.Fatal("No Export DIR Found")
	}
	export_dir = filepath.Clean(export_dir)
	if _, err := os.Open(export_dir); err != nil {
		log.Fatal("No Export DIR Found")
	}

	fmt.Println("Export to", export_dir)

	if err := db_init(); err != nil {
		log.Fatal(err.Error())
	}
	defer dbcn.Close()

	rs := dbcn.RawScan([]byte{}, []byte{}, 1000)
	if !rs.OK() {
		log.Fatal(rs.Bytex().String())
	}

	rs.KvEach(func(entry *skv.ResultEntry) int {

		if len(entry.Key) < 3 {
			return 0
		}

		if entry.Key[0] != 12 || len(entry.Value) < 2 {
			// fmt.Println(entry.Key[0], string(entry.Key[1:]), string(entry.Value[1:]))
			return 0
		}

		var (
			plen    = int(entry.Key[1])
			path    = string(entry.Key[2 : plen+2])
			name    = hex.EncodeToString(entry.Key[plen+2:])
			abspath = fmt.Sprintf("%s/%s/%03d.%s", export_dir, path, int(entry.Value[0]), name)
		)

		os.MkdirAll(export_dir+"/"+path, 0755)

		fp, err := os.OpenFile(abspath, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			fmt.Println("ERR", abspath, name, err)
			return 0
		}
		defer fp.Close()

		fp.Seek(0, 0)
		fp.Truncate(0)
		if _, err = fp.Write(entry.Value[1:]); err != nil {
			fmt.Println("ERR", path, name, err)
		}

		fmt.Println("  ", plen, path, uint8(entry.Value[0]), len(entry.Value), "  ", name)

		return 0
	})
}
