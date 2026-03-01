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

package inlog

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func Setup() {

	opts := &slog.HandlerOptions{
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				// 格式化为: 2006-01-02 15:04:05.000
				return slog.String(slog.TimeKey, a.Value.Time().Format("2006-01-02 15:04:05.000"))
			}
			if a.Key == slog.SourceKey {
				// 只保留文件名而非全路径
				if source, ok := a.Value.Any().(*slog.Source); ok {
					sourceStr := fmt.Sprintf("%s:%d", filepath.Base(source.File), source.Line)
					return slog.String(slog.SourceKey, sourceStr)
				}
			}
			return a
		},
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, opts))
	slog.SetDefault(logger)

	host, _ := os.Hostname()

	slog.Warn("inlog-setup",
		"uptime", time.Now(),
		"machine", host,
		"build", fmt.Sprintf("%s %s", runtime.Compiler, runtime.Version()),
		"os", runtime.GOOS,
		"arch", runtime.GOARCH,
	)
}
