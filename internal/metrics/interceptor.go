// Copyright 2026 Eryx <evorui at gmail dot com>, All rights reserved.
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

package metrics

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"

	"github.com/sysinner/innerstack/v2/internal/audit"
)

// UnaryInterceptor records per-method call counts and latency. Install it
// outermost (before auth and audit) so unauthenticated and denied calls are
// counted too; the wrapped interceptors cost microseconds, well inside the
// measured latency.
func UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context, req any,
		info *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
	) (any, error) {

		start := time.Now()
		resp, err := handler(ctx, req)

		method := methodName(info.FullMethod)
		rpcTotal.Add(method, resultKind(err), 1)
		rpcLatency.Add(method, "latency", time.Since(start).Seconds())

		return resp, err
	}
}

// methodName shortens "/inapi.ZoneService/ZoneInit" to
// "ZoneService/ZoneInit" for the name label.
func methodName(fullMethod string) string {
	_, method, ok := strings.Cut(fullMethod, "/inapi.")
	if !ok {
		return strings.TrimPrefix(fullMethod, "/")
	}
	return method
}

// resultKind classifies a handler outcome with the audit interceptor's
// shared StatusOf: ok | unauthenticated | denied | error.
func resultKind(err error) string {
	return audit.StatusOf(err)
}
