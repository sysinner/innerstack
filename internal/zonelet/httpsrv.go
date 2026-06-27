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
	"errors"

	"github.com/hooto/httpsrv"
	"github.com/sysinner/innerstack/v2/internal/data"
	"github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func NewPublicModule() *httpsrv.Module {
	mod := httpsrv.NewModule()
	mod.RegisterAction("/app-spec/list", httpsrvAppSpecList)
	mod.RegisterAction("/package/list", httpsrvPackageList)
	return mod
}

type AppSpecListRequest struct {
	Tags string `json:"tags"`
}

type AppSpecListResponse struct {
	Items []*inapi.AppSpec `json:"items"`
}

func httpsrvAppSpecList(ctx httpsrv.Ctx) error {

	if !status.IsZoneletLeader() {
		return errors.New("zonelet leader")
	}

	var (
		req  AppSpecListRequest
		resp AppSpecListResponse

		offset = inapi.NsAppSpec("")
	)

	defer ctx.JSON(&resp)

	ctx.Request().JsonDecode(&req)

	rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).SetLimit(1000).Exec()
	for _, v := range rs.Items {
		var item inapi.AppSpec
		if err := v.JsonDecode(&item); err != nil {
			continue
		}

		if req.Tags != "" {
			continue
		}

		resp.Items = append(resp.Items, &inapi.AppSpec{
			Name:        item.Name,
			Description: item.Description,
		})
	}

	return nil
}

func httpsrvPackageList(ctx httpsrv.Ctx) error {

	if !status.IsZoneletLeader() {
		return errors.New("zonelet leader")
	}

	var (
		req  inapi.PackageListRequest
		resp inapi.PackageListResponse

		offset = inapi.NsPackageInfo("")

		idx = map[string]*inapi.Package{}
	)

	defer ctx.JSON(&resp)

	ctx.Request().JsonDecode(&req)
	if req.Name == "" {
		req.Name = ctx.Params().Value("name")
	}
	if req.Version == "" {
		req.Version = ctx.Params().Value("version")
	}
	if req.Os == "" {
		req.Os = ctx.Params().Value("os")
	}
	if req.Arch == "" {
		req.Arch = ctx.Params().Value("arch")
	}
	if !req.LatestOnly && ctx.Params().Value("latest_only") == "true" {
		req.LatestOnly = true
	}

	rs := data.Package.NewRanger(offset, append(offset, 0xff)).SetLimit(1000).Exec()
	for _, item := range rs.Items {
		var pkg inapi.Package
		if err := item.JsonDecode(&pkg); err != nil {
			continue
		}
		if pkg.Metadata == nil || pkg.Release == nil || pkg.File == nil {
			continue
		}
		// Filter by upload status
		if pkg.File.State != inapi.PackageFileStateComplete {
			continue
		}

		// Filter by name (exact match)
		if req.Name != "" && pkg.Metadata.Name != req.Name {
			continue
		}

		// Filter by version (fuzzy match)
		if req.Version != "" &&
			!versionMatch(req.Version, pkg.Release.Version) {
			continue
		}

		// Filter by OS (exact match)
		if req.Os != "" && pkg.Release.Os != req.Os {
			continue
		}

		// Filter by arch (exact match)
		if req.Arch != "" && pkg.Release.Arch != req.Arch {
			continue
		}

		item := &inapi.Package{
			Metadata: &inapi.PackageMetadata{
				Name:        pkg.Metadata.Name,
				Version:     pkg.Metadata.Version,
				Description: pkg.Metadata.Description,
				Categories:  pkg.Metadata.Categories,
			},
			Release: &inapi.PackageRelease{
				Version: pkg.Release.Version,
				Os:      pkg.Release.Os,
				Arch:    pkg.Release.Arch,
				Built:   pkg.Release.Built,
				Size:    pkg.Release.Size,
			},
		}

		if req.Name == "" {
			if prev := idx[pkg.Metadata.Name]; prev != nil {
				if semverCompare(prev.Release.Version, pkg.Release.Version) < 0 {
					prev.Metadata = item.Metadata
					prev.Release = item.Release
				}
				continue
			}
		}

		idx[pkg.Metadata.Name] = item
		resp.Items = append(resp.Items, item)
	}

	// If latest_only is true, keep only the latest version for each (name, os, arch) combination
	if req.LatestOnly && len(resp.Items) > 0 {
		resp.Items = filterLatestPackages(resp.Items)
	}

	return nil
}
