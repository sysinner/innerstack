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

package inutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func FsMakeFileDir(path string, uid, gid int, mode os.FileMode) error {

	if idx := strings.LastIndex(path, "/"); idx > 0 {
		return FsMakeDir(path[0:idx], uid, gid, mode)
	}

	return nil
}

func FsMakeDir(path string, uid, gid int, mode os.FileMode) error {

	if _, err := os.Stat(path); err == nil {
		return nil
	}

	if runtime.GOOS == "linux" && (uid < 500 || gid < 500) {
		return errors.New("Invalid uid or gid")
	}

	paths, path := strings.Split(strings.Trim(path, "/"), "/"), ""

	for _, v := range paths {

		path += "/" + v

		if _, err := os.Stat(path); err == nil {
			continue
		}

		if err := os.Mkdir(path, mode); err != nil {
			return err
		}

		os.Chmod(path, mode)
		os.Chown(path, uid, gid)
	}

	return nil
}

func FsWrite(file string, bs []byte) error {

	fp, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE, 0640)
	if err != nil {
		if os.IsNotExist(err) {
			if err = os.Mkdir(filepath.Dir(file), 0640); err != nil {
				return err
			}
			fp, err = os.OpenFile(file, os.O_RDWR|os.O_CREATE, 0640)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	defer fp.Close()

	fp.Seek(0, 0)
	fp.Truncate(0)

	if _, err = fp.Write(bs); err == nil {
		err = fp.Sync()
	}

	return err
}
