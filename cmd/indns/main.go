// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
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
	"log/slog"
	"os"
	"os/signal"
	"runtime"

	"github.com/sysinner/innerstack/v2/internal/indns/config"
	"github.com/sysinner/innerstack/v2/internal/indns/server"
	"github.com/sysinner/innerstack/v2/pkg/inlog"
)

func init() {
	runtime.GOMAXPROCS(1)
	inlog.Setup()
}

var (
	version = "git"
)

func main() {

	if err := config.Setup(version); err != nil {
		slog.Error("config setup fail : " + err.Error())
		os.Exit(1)
	}

	if err := server.Start(); err != nil {
		slog.Error("server start fail : " + err.Error())
		os.Exit(1)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	sg := <-sig
	slog.Warn("server signal quit " + sg.String())
}
