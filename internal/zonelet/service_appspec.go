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

package zonelet

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/sysinner/innerstack/v2/internal/data"
	"github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inauth"
)

// AppSpecList lists application specifications stored in the zone. When the
// request name is set, only the spec with that exact name is returned via a
// direct key read; otherwise all specs are returned via a prefix range scan.
func (s *zoneServer) AppSpecList(
	ctx context.Context, req *inapi.AppSpecListRequest,
) (*inapi.AppSpecListResponse, error) {

	if !inauth.AppContext(ctx).Allow(inapi.AuthScope_App_Read) {
		return nil, errors.New("auth fail: missing app:ro scope")
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	resp := &inapi.AppSpecListResponse{}

	// Fast path: lookup a single spec by name (the logical key).
	if req.Name != "" {
		if err := inapi.DNSLabelValid(req.Name); err != nil {
			return nil, fmt.Errorf("name: %w", err)
		}

		var spec inapi.AppSpec
		if rs := data.Zonelet.NewReader(inapi.NsAppSpec(req.Name)).Exec(); !rs.OK() {
			if rs.NotFound() {
				return resp, nil
			}
			return nil, rs.Error()
		} else if err := rs.Item().JsonDecode(&spec); err != nil {
			return nil, err
		}

		resp.Items = append(resp.Items, &spec)
		return resp, nil
	}

	// List all specs via prefix range scan.
	offset := inapi.NsAppSpec("")
	rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).Exec()
	for _, item := range rs.Items {
		var spec inapi.AppSpec
		if err := item.JsonDecode(&spec); err == nil {
			resp.Items = append(resp.Items, &spec)
		}
	}

	return resp, nil
}

// appSpecSplitVersion splits an AppSpec version of the form
// "MAJOR.MINOR.PATCH[-N]" into its main version ("MAJOR.MINOR.PATCH") and a
// numeric release number N. The release number is the trailing "-N" suffix
// where N is a non-negative integer; a version without such a suffix has a
// release number of 0. Non-numeric suffixes (e.g. "-alpha") are kept as part
// of the main version.
//
// Under this scheme a bare version is the oldest release of its main version:
// 0.0.1 (release 0) < 0.0.1-1 < 0.0.1-2.
func appSpecSplitVersion(v string) (main string, release int) {
	if i := strings.LastIndex(v, "-"); i >= 0 {
		if n, err := strconv.Atoi(v[i+1:]); err == nil && n >= 0 {
			return v[:i], n
		}
	}
	return v, 0
}

// appSpecCompareMain compares the main (MAJOR.MINOR.PATCH) parts of two AppSpec
// versions using SemVer ordering. Returns -1, 0 or 1.
func appSpecCompareMain(a, b string) int {
	if !strings.HasPrefix(a, "v") {
		a = "v" + a
	}
	if !strings.HasPrefix(b, "v") {
		b = "v" + b
	}
	return semver.Compare(a, b)
}

// appSpecResolveVersion resolves and validates the requested AppSpec version
// against a previously persisted version (if any).
//
//   - When no previous spec exists, the request version is used as-is after
//     validation. An empty request version defaults to "0.0.1".
//   - When a previous spec exists, the main versions (MAJOR.MINOR.PATCH) are
//     compared:
//   - equal: the release number is incremented on top of the previous
//     version (e.g. "0.0.1" -> "0.0.1-1", "0.0.1-1" -> "0.0.1-2").
//   - greater: the request version is used as-is.
//   - lower: an error is returned.
//
// The request version's own release number is ignored when the main versions
// are equal; the result is always derived from the previous release number.
func appSpecResolveVersion(reqVersion, prevVersion string, prevExists bool) (string, error) {

	v := reqVersion
	if v == "" {
		v = "0.0.1"
	} else if err := inapi.SemverValid(v); err != nil {
		return "", fmt.Errorf("spec.version: %w", err)
	}

	if !prevExists || prevVersion == "" {
		return v, nil
	}

	reqMain, _ := appSpecSplitVersion(v)
	prevMain, prevRel := appSpecSplitVersion(prevVersion)

	switch c := appSpecCompareMain(reqMain, prevMain); {
	case c == 0:
		return fmt.Sprintf("%s-%d", prevMain, prevRel+1), nil
	case c > 0:
		return v, nil
	default:
		return "", fmt.Errorf("spec.version %q is lower than existing %q", v, prevVersion)
	}
}
