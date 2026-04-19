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
	"log"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/auth"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/data"
	"github.com/sysinner/incore/v2/internal/hostlet"
	"github.com/sysinner/incore/v2/internal/server"
	"github.com/sysinner/incore/v2/internal/zonelet"
	"github.com/sysinner/incore/v2/pkg/inlog"
	"github.com/sysinner/incore/v2/pkg/signals"
)

var (
	version = "v2.0.0-alpha.01"
	release = "git"
)

func main() {

	inlog.Setup()

	if err := config.Setup(version, release); err != nil {
		log.Fatalf("incore/config init error %s", err.Error())
	}

	if err := auth.Setup(); err != nil {
		log.Fatalf("auth/setup init error %s", err.Error())
	}

	if err := data.Setup(); err != nil {
		log.Fatalf("incore/data init error %s", err.Error())
	}
	defer data.Close()

	if err := server.Setup(); err != nil {
		log.Fatalf("incore/server setup error %s", err.Error())
	}

	if err := server.RegisterServer(func(s *server.RpcServer) {
		inapi.RegisterHostInternalServiceServer(s, hostlet.NewInternalServer())
		inapi.RegisterZoneServiceServer(s, zonelet.NewServer())
		inapi.RegisterZoneInternalServiceServer(s, zonelet.NewInternalServer())
	}); err != nil {
		log.Fatalf("incore/server start error %s", err.Error())
	}

	signals.Go(server.Run, server.Close)

	if err := hostlet.TryRun(); err != nil {
		log.Fatalf("hostlet start error %s", err.Error())
	}
	signals.Go(hostlet.Run, nil)

	signals.Go(zonelet.Run, nil)

	signals.Wait()
}
