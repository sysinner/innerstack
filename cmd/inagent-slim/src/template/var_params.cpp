// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include "template/var_params.h"

#include "util/string_util.h"

namespace inagent {
    namespace template_ {

        static std::string keyenc(const std::string& k) {
            auto s = util::replace_all(k, "/", ".");
            return util::replace_all(s, "-", "_");
        }

        static void addr_export(std::map<std::string, std::string>& sets,
                                const std::string& prefix,
                                const std::string& host, uint32_t port) {
            sets[prefix + "_addr"] =
                util::fmt_sprintf("%s:%u", host.c_str(), port);
            sets[prefix + "_host"] = host;
            sets[prefix + "_port"] = std::to_string(port);
        }

        static void endpoint_export(std::map<std::string, std::string>& sets,
                                    const std::string& prefix,
                                    const std::string& app_name,
                                    const std::string& domain, uint32_t port) {
            sets[prefix + "_addr"] = util::fmt_sprintf(
                "%s.%s:%u", app_name.c_str(), domain.c_str(), port);
            sets[prefix + "_host"] =
                util::fmt_sprintf("%s.%s", app_name.c_str(), domain.c_str());
            sets[prefix + "_port"] = std::to_string(port);
        }

        std::map<std::string, std::string> var_params(
            const model::AppReplicaInstance& app) {
            std::map<std::string, std::string> sets;

            // app identity: use instance name (meta.name) as the logical key
            sets["app.name"] = app.app.meta.name;
            sets["app.replica.rep_id"] = std::to_string(app.replica.id);
            sets["app.deploy.replica_cap"] =
                std::to_string(app.app.deploy.replica_cap);

            // dependency configs and network
            for (const auto& dep : app.app.deploy.depends) {
                if (dep.spec_name.empty()) continue;

                for (const auto& item : dep.configs) {
                    if (item.name.empty()) continue;
                    std::string cfg_name = keyenc(item.name);
                    sets[util::fmt_sprintf("deps.%s.cfg.%s",
                                           dep.spec_name.c_str(),
                                           cfg_name.c_str())] = item.value;
                    for (const auto& item2 : item.items) {
                        sets[util::fmt_sprintf(
                            "deps.%s.cfg.%s.%s", dep.spec_name.c_str(),
                            cfg_name.c_str(), keyenc(item2.name).c_str())] =
                            item2.value;
                    }
                }

                for (const auto& rep : dep.replicas) {
                    for (const auto& sp : rep.service_ports) {
                        if (sp.port < 1 || sp.port > 65535) continue;
                        std::string key = util::fmt_sprintf(
                            "deps.%s.net.%s.internal", dep.spec_name.c_str(),
                            sp.name.c_str());
                        if (!rep.vpc_ipv4.empty()) {
                            addr_export(sets, key, rep.vpc_ipv4, sp.port);
                        } else if (sp.host_port > 0 && !rep.host_ipv4.empty()) {
                            addr_export(sets, key, rep.host_ipv4, sp.host_port);
                        } else {
                            addr_export(sets, key, "127.0.0.1", sp.port);
                        }
                        if (!app.zone_base_domain.empty()) {
                            endpoint_export(
                                sets,
                                util::fmt_sprintf("deps.%s.net.%s.service",
                                                  dep.spec_name.c_str(),
                                                  sp.name.c_str()),
                                dep.instance_name, app.zone_base_domain,
                                sp.port);
                        }
                    }
                }
            }

            // self configs
            for (const auto& item : app.app.deploy.configs) {
                if (item.name.empty()) continue;
                std::string cfg_name = keyenc(item.name);
                sets[util::fmt_sprintf("self.cfg.%s", cfg_name.c_str())] =
                    item.value;
                for (const auto& item2 : item.items) {
                    sets[util::fmt_sprintf("self.cfg.%s.%s", cfg_name.c_str(),
                                           keyenc(item2.name).c_str())] =
                        item2.value;
                }
            }

            // self network
            for (const auto& sp : app.replica.service_ports) {
                if (sp.port < 1 || sp.port > 65535) continue;
                std::string key =
                    util::fmt_sprintf("self.net.%s.internal", sp.name.c_str());
                if (!app.replica.vpc_ipv4.empty()) {
                    addr_export(sets, key, app.replica.vpc_ipv4, sp.port);
                } else if (sp.host_port > 0 && !app.replica.host_ipv4.empty()) {
                    addr_export(sets, key, app.replica.host_ipv4, sp.host_port);
                } else {
                    addr_export(sets, key, "127.0.0.1", sp.port);
                }
                if (!app.zone_base_domain.empty()) {
                    endpoint_export(sets,
                                    util::fmt_sprintf("self.net.%s.service",
                                                      sp.name.c_str()),
                                    app.app.meta.name, app.zone_base_domain,
                                    sp.port);
                }
            }

            // packages
            for (const auto& p : app.app.spec.packages) {
                std::string pkg_name = util::replace_all(p.name, "-", "_");
                sets[util::fmt_sprintf("ipk.%s.path", pkg_name.c_str())] =
                    util::fmt_sprintf("/usr/innerstack/%s", p.name.c_str());
            }

            return sets;
        }

    } // namespace template_
} // namespace inagent