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

package inconf

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/sysinner/incore/v2/internal/inutil"
	"github.com/sysinner/incore/v2/pkg/inapi"
)

const (
	confFilePath = "/home/action/.sysinner/app_instance.json"
)

type AppConfigHelper struct {
	*inapi.AppReplicaInstance
	updated int64
}

func NewAppConfigHelper() (*AppConfigHelper, error) {

	st, err := os.Stat(confFilePath)
	if err != nil {
		return nil, err
	}

	var app inapi.AppReplicaInstance

	if err := inutil.JsonDecodeFromFile(confFilePath, &app); err != nil {
		return nil, err
	}

	if app.App == nil || app.App.Spec == nil ||
		app.App.Deploy == nil || app.Replica == nil {
		return nil, errors.New("Not App Instance Setup")
	}

	return &AppConfigHelper{
		AppReplicaInstance: &app,
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

func (it *AppConfigHelper) Config(cfgName string) *inapi.AppDeployConfigItem {
	cfgItems := strings.Split(cfgName, ".")
	if len(cfgItems) > 0 {
		for _, item := range it.App.Deploy.Configs {
			// if prefixMatch(item.Name, cfgItems[0]) {
			if item.Name == cfgItems[0] {
				if len(cfgItems) > 1 {
					for _, item := range item.Items {
						if item.Name == cfgItems[1] {
							return item
						}
					}
				} else {
					return item
				}
			}
		}
	}
	return nil
}

func (it *AppConfigHelper) ConfigValue(cfgName string) string {
	if item := it.Config(cfgName); item != nil {
		return item.Value
	}
	return ""
}

func (it *AppConfigHelper) ConfigValueOK(cfgName string) (string, bool) {
	if item := it.Config(cfgName); item != nil {
		return item.Value, true
	}
	return "", false
}

func (it *AppConfigHelper) ServiceQuery(qs ...string) *inapi.AppDeployServicePort {

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

func (it *AppConfigHelper) Service(name string, port uint32) *inapi.AppDeployServicePort {
	for _, rep := range it.App.Deploy.Replicas {
		for _, v := range rep.ServicePorts {
			if port > 0 && v.Port != port {
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

func (app *AppConfigHelper) Params() map[string]string {
	return VarParams(app.AppReplicaInstance)
}

// 扁平化的配置信息导出
func VarParams(app *inapi.AppReplicaInstance) map[string]string {

	sets := map[string]string{}

	sets["app.id"] = app.App.InstanceId()
	sets["app.replica.rep_id"] = fmt.Sprintf("%d", app.Replica.Id)
	sets["app.deploy.replica_cap"] = fmt.Sprintf("%d", app.App.Deploy.ReplicaCap)

	addrExport := func(prefix, host string, port uint32) {
		sets[prefix+"_addr"] = fmt.Sprintf("%s:%d", host, port)
		sets[prefix+"_host"] = fmt.Sprintf("%s", host)
		sets[prefix+"_port"] = fmt.Sprintf("%d", port)
	}

	endpointExport := func(prefix, appId, specName string, port uint32) {
		sets[prefix+"_addr"] = fmt.Sprintf("app-%s.%s:%d", appId, specName, port)
		sets[prefix+"_host"] = fmt.Sprintf("app-%s.%s", appId, specName)
		sets[prefix+"_port"] = fmt.Sprintf("%d", port)
	}

	// 依赖 AppSpec 的配置数据
	for _, dep := range app.App.Deploy.Depends {

		if dep.SpecName == "" {
			continue
		}

		for _, item := range dep.Configs {
			if item.Name == "" {
				continue
			}
			cfgName := keyenc(item.Name)
			sets[fmt.Sprintf("deps.%s.cfg.%s", dep.SpecName, cfgName)] = item.Value
			for _, item2 := range item.Items {
				sets[fmt.Sprintf("deps.%s.cfg.%s.%s", dep.SpecName, cfgName, keyenc(item2.Name))] = item2.Value
			}
		}

		for _, rep := range dep.Replicas {

			for _, sp := range rep.ServicePorts {
				if sp.Port < 1 || sp.Port > 65535 {
					continue
				}
				key := fmt.Sprintf("deps.%s.net.%s.internal", dep.SpecName, sp.Name)
				if rep.VpcIpv4 != "" {
					addrExport(key, rep.VpcIpv4, sp.Port)
				} else if sp.HostPort > 0 && rep.HostIpv4 != "" {
					addrExport(key, rep.HostIpv4, sp.HostPort)
				} else {
					addrExport(key, "127.0.0.1", sp.Port)
				}
				if app.ZoneBaseDomain != "" {
					endpointExport(
						fmt.Sprintf("deps.%s.net.%s.service", dep.SpecName, sp.Name),
						dep.InstanceId, app.ZoneBaseDomain, sp.Port)
				}
			}
		}
	}

	// 当前 App 配置数据
	for _, item := range app.App.Deploy.Configs {
		if item.Name == "" {
			continue
		}
		cfgName := keyenc(item.Name)
		sets[fmt.Sprintf("self.cfg.%s", cfgName)] = item.Value
		for _, item2 := range item.Items {
			sets[fmt.Sprintf("self.cfg.%s.%s", cfgName, keyenc(item2.Name))] = item2.Value
		}
	}

	for _, sp := range app.Replica.ServicePorts {
		if sp.Port < 1 || sp.Port > 65535 {
			continue
		}
		key := fmt.Sprintf("self.net.%s.internal", sp.Name)
		if app.Replica.VpcIpv4 != "" {
			addrExport(key, app.Replica.VpcIpv4, sp.Port)
		} else if sp.HostPort > 0 && app.Replica.HostIpv4 != "" {
			addrExport(key, app.Replica.HostIpv4, sp.HostPort)
		} else {
			addrExport(key, "127.0.0.1", sp.Port)
		}
		if app.ZoneBaseDomain != "" {
			endpointExport(
				fmt.Sprintf("self.net.%s.service", sp.Name),
				app.App.InstanceId(), app.ZoneBaseDomain, sp.Port)
		}
	}

	// packages
	for _, p := range app.App.Spec.Packages {
		sets[fmt.Sprintf("ipk.%s.path", strings.Replace(p.Name, "-", "_", -1))] =
			fmt.Sprintf("/usr/instack/%s", p.Name)
	}

	return sets
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

func keyenc(k string) string {
	return strings.Replace(strings.Replace(k, "/", ".", -1), "-", "_", -1)
}

func RenderWithExpand(text string, sets map[string]string) string {
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	text2 := re.ReplaceAllStringFunc(text, func(match string) string {
		// match is the full matched string, e.g. "${app.name}" or "${NAME}".
		// Extract the variable name by stripping the leading "${" and trailing "}".
		key := match[2 : len(match)-1]

		// Replace with the value if the key exists in the data source.
		if val, exists := sets[key]; exists {
			return val
		}

		// Key not found; preserve the original match unchanged.
		return match
	})
	return text2
}

func FileRender(dstFile, srcFile string, sets map[string]string, perm os.FileMode) error {

	fpsrc, err := os.Open(srcFile)
	if err != nil {
		return err
	}
	defer fpsrc.Close()

	src, err := io.ReadAll(fpsrc)
	if err != nil {
		return err
	}

	text := RenderWithExpand(string(src), sets)

	fpdst, err := os.OpenFile(dstFile, os.O_RDWR|os.O_CREATE, perm)
	if err != nil {
		return err
	}
	defer fpdst.Close()

	fpdst.Seek(0, 0)
	fpdst.Truncate(0)

	_, err = fpdst.Write([]byte(text))
	return err
}
