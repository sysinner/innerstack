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
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/sysinner/innerstack/v2/pkg/inmetrics"
)

func TestMethodName(t *testing.T) {
	tests := []struct {
		full, want string
	}{
		{"/inapi.ZoneService/ZoneInit", "ZoneService/ZoneInit"},
		{"/inapi.ZoneInternalService/HostStatusUpdate",
			"ZoneInternalService/HostStatusUpdate"},
		{"/other.Package/Method", "other.Package/Method"},
	}
	for _, tt := range tests {
		if got := methodName(tt.full); got != tt.want {
			t.Errorf("methodName(%q) = %q, want %q", tt.full, got, tt.want)
		}
	}
}

func TestResultKind(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{nil, "ok"},
		{grpcstatus.Error(codes.Unauthenticated, "x"), "unauthenticated"},
		{grpcstatus.Error(codes.PermissionDenied, "x"), "denied"},
		{grpcstatus.Error(codes.NotFound, "x"), "error"},
		{errors.New("plain"), "error"},
	}
	for _, tt := range tests {
		if got := resultKind(tt.err); got != tt.want {
			t.Errorf("resultKind(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestUnaryInterceptorCounts(t *testing.T) {

	call := func(method string, handlerErr error) {
		ic := UnaryInterceptor()
		_, err := ic(
			context.Background(), struct{}{},
			&grpc.UnaryServerInfo{FullMethod: "/inapi." + method},
			func(ctx context.Context, req any) (any, error) {
				return nil, handlerErr
			},
		)
		if err != handlerErr && handlerErr == nil && err != nil {
			t.Fatalf("handler error not propagated: %v", err)
		}
	}

	// Distinct methods keep the global registry assertions independent.
	call("TestSvc/OkOne", nil)
	call("TestSvc/DeniedOne", grpcstatus.Error(codes.PermissionDenied, "x"))
	call("TestSvc/UnauthOne", grpcstatus.Error(codes.Unauthenticated, "x"))
	call("TestSvc/ErrorOne", errors.New("boom"))

	var buf bytes.Buffer
	inmetrics.WriteTo(&buf)
	body := buf.String()

	// inmetrics renders label pairs sorted by label name (item, name).
	want := []string{
		`innerstack_rpc_total{item="ok",name="TestSvc/OkOne"} 1`,
		`innerstack_rpc_total{item="denied",name="TestSvc/DeniedOne"} 1`,
		`innerstack_rpc_total{item="unauthenticated",name="TestSvc/UnauthOne"} 1`,
		`innerstack_rpc_total{item="error",name="TestSvc/ErrorOne"} 1`,
	}
	for _, s := range want {
		if !strings.Contains(body, s) {
			t.Fatalf("body missing %q:\n%s", s, body)
		}
	}
	if !strings.Contains(body, `innerstack_rpc_latency_seconds_bucket{`) {
		t.Fatalf("latency histogram missing from body:\n%s", body)
	}
}
