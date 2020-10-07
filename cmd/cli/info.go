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
	"reflect"
	"strings"

	"github.com/hooto/htoml4g/htoml"

	"github.com/sysinner/incore/config"
)

type infoCommand struct {
	cmd              *baseCommand
	argShowSecretKey bool
}

func NewInfoCommand() *baseCommand {

	c := &infoCommand{
		cmd: &baseCommand{
			Use:   "info",
			Short: "output system information",
		},
	}

	c.cmd.FParseErrWhitelist.UnknownFlags = false

	c.cmd.Flags().BoolVarP(&c.argShowSecretKey, "show-secretkey", "s",
		false,
		"output the full secret key content")

	c.cmd.RunE = c.run

	return c.cmd
}

func (it *infoCommand) run(cmd *baseCommand, args []string) error {

	var rep config.HostInfoReply
	if err := localApiCommand("system/host-info", nil, &rep); err != nil {
		return err
	}

	if rep.Host.Id == "" {
		return fmt.Errorf("no info found")
	}

	opts := htoml.NewEncodeOptions()
	if !it.argShowSecretKey {
		opts.Filters = append(opts.Filters, func(key htoml.Key, val reflect.Value) reflect.Value {
			if strings.Contains(key.String(), "secret") {
				switch val.Kind() {
				case reflect.String:
					if val.Len() > 10 {
						str := val.String()
						return reflect.ValueOf(str[:4] + "********" + str[len(str)-4:])
					}
				}
			}
			return val
		})
	}

	bs, _ := htoml.Encode(rep, opts)

	fmt.Println(string(bs))

	return nil
}
