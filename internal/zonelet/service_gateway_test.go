// Copyright 2015 Eryx <evoruri at gmail dot com>, All rights reserved.
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

package zonelet

import (
	"strings"
	"testing"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// TestGatewayIngressDeleteEligible verifies the delete gate for
// action=delete: only a disabled record whose last operation
// (Meta.Updated) is older than GatewayIngressDeleteDelaySeconds
// may be physically removed.
func TestGatewayIngressDeleteEligible(t *testing.T) {

	const now int64 = 1_800_000_000

	fixtures := []struct {
		name     string
		action   string
		ageSecs  int64 // age of the last operation relative to now
		wantErr  bool
		errParts []string // substrings expected in the error message
	}{
		{
			name:    "enabled_record_rejected",
			action:  inapi.GatewayIngressActionEnable,
			ageSecs: 30 * 86400,
			wantErr: true,
			errParts: []string{
				"disable it first",
			},
		},
		{
			name:    "empty_action_rejected",
			action:  "",
			ageSecs: 30 * 86400,
			wantErr: true,
			errParts: []string{
				"disable it first",
			},
		},
		{
			name:    "disabled_recently_rejected",
			action:  inapi.GatewayIngressActionDisable,
			ageSecs: 1 * 86400,
			wantErr: true,
			errParts: []string{
				"deletion requires more than",
			},
		},
		{
			name:    "disabled_at_exact_delay_rejected",
			action:  inapi.GatewayIngressActionDisable,
			ageSecs: inapi.GatewayIngressDeleteDelaySeconds,
			wantErr: true,
			errParts: []string{
				"deletion requires more than",
			},
		},
		{
			name:    "disabled_just_past_delay_allowed",
			action:  inapi.GatewayIngressActionDisable,
			ageSecs: inapi.GatewayIngressDeleteDelaySeconds + 1,
		},
		{
			name:    "disabled_long_ago_allowed",
			action:  inapi.GatewayIngressActionDisable,
			ageSecs: 365 * 86400,
		},
	}

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {

			item := &inapi.GatewayIngress{
				Meta: &inapi.Metadata{
					Updated: now - fx.ageSecs,
				},
				Domain: "example.com",
				Action: fx.action,
			}

			err := gatewayIngressDeleteEligible(item, now)

			if fx.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				for _, part := range fx.errParts {
					if !strings.Contains(err.Error(), part) {
						t.Fatalf("error %q does not contain %q", err.Error(), part)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}

	// Sanity: the delay constant must stay at 10 whole days so the boundary
	// cases above remain meaningful.
	if inapi.GatewayIngressDeleteDelaySeconds != 10*24*3600 {
		t.Fatalf("GatewayIngressDeleteDelaySeconds = %d seconds, want %d",
			inapi.GatewayIngressDeleteDelaySeconds, 10*24*3600)
	}
}
