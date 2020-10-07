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

	"github.com/lessos/lessgo/crypto/idhash"

	"github.com/sysinner/incore/config"
	"github.com/sysinner/incore/inapi"
)

type zoneInitCommand struct {
	cmd        *baseCommand
	argRequest config.ZoneInitRequest
}

func NewZoneInitCommand() *baseCommand {

	c := &zoneInitCommand{
		cmd: &baseCommand{
			Use:   "zone-init",
			Short: "initialize a zone cluster",
			Long:  `Perform one-time-only initialization of a zone cluster`,
		},
	}

	c.cmd.Flags().StringVarP(&c.argRequest.HostAddr, "host-addr", "",
		"",
		`the ip must be a LAN (Local Area Network) and in range of:
  10.0.0.0 ~ 10.255.255.255,
  172.16.0.0 ~ 172.31.255.255,
  192.168.0.0 ~ 192.168.255.25.
if the port number is left unspecified, it defaults to 9529.
	`)

	c.cmd.Flags().StringVarP(&c.argRequest.WanAddr, "wan-ip", "",
		"",
		`the WAN (Wide Area Network) IP`,
	)

	c.cmd.Flags().StringVarP(&c.argRequest.ZoneId, "zone-id", "",
		"z1",
		"the name of zone")

	c.cmd.Flags().StringVarP(&c.argRequest.CellId, "cell-id", "",
		"g1",
		"the name of host group")

	c.cmd.Flags().Uint16VarP(&c.argRequest.HttpPort, "http-port", "",
		9530,
		"the http port for zone's web console and api")

	c.cmd.Flags().StringVarP(&c.argRequest.Password, "password", "",
		"",
		"the access password for the sysadmin (super system administrator)")

	c.cmd.RunE = c.run

	return c.cmd
}

func (it *zoneInitCommand) run(cmd *baseCommand, args []string) error {

	if err := rootAllow(); err != nil {
		return err
	}

	if it.argRequest.Password == "" {
		it.argRequest.Password = idhash.RandHexString(32)
	}

	var rep inapi.WebServiceReply
	if err := localApiCommand("config/zone-init", it.argRequest, &rep); err != nil {
		return err
	}

	if rep.Kind != "OK" {
		return fmt.Errorf("Fail: %s\n", rep.Message)
	}

	ip := inapi.HostNodeAddress(it.argRequest.HostAddr).IP()
	if it.argRequest.WanAddr != "" {
		ip = it.argRequest.WanAddr
	}

	fmt.Println("zone successfully initialized")
	fmt.Printf("  inPanel Management\n")
	fmt.Printf("       url: http://%s:%d/in/\n", ip, it.argRequest.HttpPort)
	fmt.Printf("      user: %s\n", "sysadmin")
	fmt.Printf("  password: %s\n", it.argRequest.Password)

	return nil
}
