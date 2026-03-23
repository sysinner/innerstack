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

package inconf

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/inutil"
)

const (
	confFilePath = "/home/action/.sysinner/app_instance.json"
)

type AppConfigHelper struct {
	AppReplicaInstance
	updated int64
}

type AppConfigGroup = inapi.AppDeployOption

type AppReplicaInstance struct {
	App     *inapi.AppInstance      `json:"app"`
	Replica *inapi.AppDeployReplica `json:"replica"`
}

func NewAppConfigHelper() (*AppConfigHelper, error) {

	st, err := os.Stat(confFilePath)
	if err != nil {
		return nil, err
	}

	var app AppReplicaInstance

	if err := inutil.JsonDecodeFromFile(confFilePath, &app); err != nil {
		return nil, err
	}

	if app.App == nil || app.App.Spec == nil ||
		app.App.Deploy == nil || app.Replica == nil {
		return nil, errors.New("Not App Instance Setup")
	}

	return &AppConfigHelper{
		AppReplicaInstance: app,
		updated:            st.ModTime().UnixMilli(),
	}, nil
}

func (it *AppConfigHelper) Update() bool {
	if st, err := os.Stat(confFilePath); err == nil && st.ModTime().UnixMilli() > it.updated {
		it.updated = st.ModTime().UnixMilli()
		return true
	}
	return false
}

func (it *AppConfigHelper) Spec() *inapi.AppSpec {
	return it.App.Spec
}

func (it *AppConfigHelper) ConfigQuery(cfgGroupNames ...string) *AppConfigGroup {

	for _, v := range cfgGroupNames {
		if cfg := it.Config(v); cfg != nil {
			return cfg
		}
	}

	return nil
}

func (it *AppConfigHelper) Config(cfgGroupName string) *AppConfigGroup {
	for _, opt := range it.App.Deploy.Options {
		if prefixMatch(opt.Name, cfgGroupName) {
			return (*AppConfigGroup)(opt)
		}
	}
	return nil
}

func (it *AppConfigHelper) ConfigField(cfgGroupName, cfgItemName string) *inapi.AppDeployOptionField {
	if opt := it.Config(cfgGroupName); opt != nil {
		return opt.Field(cfgItemName)
	}
	return nil
}

func (it *AppConfigHelper) ConfigValue(cfgGroupName, cfgItemName string) string {
	if opt := it.Config(cfgGroupName); opt != nil {
		return opt.Value(cfgItemName)
	}
	return ""
}

func (it *AppConfigHelper) ConfigValueOK(cfgGroupName, cfgItemName string) (string, bool) {
	if opt := it.Config(cfgGroupName); opt != nil {
		return opt.ValueOK(cfgItemName)
	}
	return "", false
}

func (it *AppConfigHelper) ServiceQuery(qs ...string) *inapi.AppServicePort {

	for _, q := range qs {

		var (
			ar   = strings.Split(q, ";")
			name = ""
			port = 0
		)

		for _, qv := range ar {

			qvs := strings.Split(qv, "=")
			if len(qvs) != 2 {
				continue
			}

			switch qvs[0] {

			case "name":
				name = qvs[1]

			case "port":
				port, _ = strconv.Atoi(qvs[1])
			}
		}

		if srv := it.Service(name, uint32(port)); srv != nil {
			return srv
		}
	}

	return nil
}

func (it *AppConfigHelper) Service(name string, port uint32) *inapi.AppServicePort {

	if len(it.App.Deploy.Services) > 0 {

		for _, v := range it.App.Deploy.Services {

			if port > 0 && v.Port != port {
				continue
			}

			if len(v.Endpoints) < 1 {
				continue
			}

			if name != "" &&
				!prefixMatch(v.Name, name) &&
				v.Name != name {
				continue
			}

			return v
		}
	}

	return nil
}

func prefixMatch(s1, s2 string) bool {
	if len(s1) > 0 && s1 == s2 {
		return true
	} else if len(s2) > 1 && s2[len(s2)-1] == '*' {
		if strings.HasPrefix(s1, s2[:len(s2)-1]) {
			return true
		}
	}
	return false
}
