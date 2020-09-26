// Copyright 2019 Eryx <evorui аt gmail dοt com>, All rights reserved.
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
	"fmt"
	"os/user"
	"strings"

	"github.com/lessos/lessgo/encoding/json"
	"github.com/lessos/lessgo/net/httpclient"
)

func rootAllow() error {
	if u, err := user.Current(); err != nil || u.Uid != "0" {
		return fmt.Errorf("Access Denied : must be run as root")
	}
	return nil
}

func localApiCommand(path string, req, rep interface{}) error {

	path = strings.TrimLeft(path, "/")

	hc := httpclient.Post(fmt.Sprintf("http://unix.sock/in/o1/%s", path))
	defer hc.Close()

	hc.SetUnixDomainSocket(fmt.Sprintf("%s/var/server.sock", Prefix))

	if req != nil {
		js, _ := json.Encode(req, "")
		hc.Body(js)
	}

	if err := hc.ReplyJson(rep); err != nil {
		return err
	}

	return nil
}
