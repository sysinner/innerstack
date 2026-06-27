// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include "config_merge/merger.h"

#include <yaml-cpp/yaml.h>

#include <nlohmann/json.hpp>
#include <toml.hpp>

#include "util/fs.h"
#include "util/string_util.h"

namespace inagent {
    namespace config_merge {

        ConfigType infer_type_from_ext(const std::string& path) {
            auto pos = path.find_last_of('.');
            if (pos == std::string::npos) return ConfigType::UNKNOWN;
            std::string ext = path.substr(pos);
            for (auto& c : ext)
                c = static_cast<char>(tolower(static_cast<unsigned char>(c)));

            if (ext == ".json") return ConfigType::JSON;
            if (ext == ".toml") return ConfigType::TOML;
            if (ext == ".yaml" || ext == ".yml") return ConfigType::YAML;
            if (ext == ".ini" || ext == ".cfg" || ext == ".conf")
                return ConfigType::INI;
            if (ext == ".properties") return ConfigType::JAVA_PROP;
            return ConfigType::UNKNOWN;
        }

        static int merge_json(const std::string& target_file,
                              const std::string& field_value) {
            // Match Go (viper.ReadInConfig): the target file must already
            // exist.
            if (!util::file_exists(target_file)) return -1;

            nlohmann::json existing;
            try {
                existing = nlohmann::json::parse(util::read_file(target_file));
            } catch (...) {
                existing = nlohmann::json::object();
            }

            nlohmann::json incoming;
            try {
                incoming = nlohmann::json::parse(field_value);
            } catch (...) {
                return -1;
            }

            existing.merge_patch(incoming);
            std::string output = existing.dump(2) + "\n";
            return util::write_file(target_file, output, 0644) ? 0 : -1;
        }

        static void toml_merge(toml::table& dst, const toml::table& src);

        static int merge_toml(const std::string& target_file,
                              const std::string& field_value) {
            if (!util::file_exists(target_file)) return -1;

            toml::value existing;
            try {
                existing = toml::parse(target_file);
            } catch (...) {
                existing = toml::table();
            }

            toml::value incoming;
            try {
                std::istringstream iss(field_value);
                incoming = toml::parse(iss, "incoming");
            } catch (...) {
                return -1;
            }

            if (!incoming.is_table() || !existing.is_table()) return -1;

            toml_merge(existing.as_table(), incoming.as_table());

            std::ostringstream oss;
            oss << existing;
            return util::write_file(target_file, oss.str(), 0644) ? 0 : -1;
        }

        static void toml_merge(toml::table& dst, const toml::table& src) {
            for (const auto& kv : src) {
                const std::string& k = kv.first;
                const toml::value& v = kv.second;
                if (v.is_table()) {
                    auto it = dst.find(k);
                    if (it != dst.end() && it->second.is_table()) {
                        toml_merge(it->second.as_table(), v.as_table());
                        continue;
                    }
                }
                dst[k] = v;
            }
        }

        // YAML::Node is a shared_ptr-style wrapper, so passing by value still
        // aliases the underlying data; operator[] returns an rvalue Node that
        // cannot bind to a non-const reference, hence the by-value parameter.
        static void yaml_merge(YAML::Node target, const YAML::Node& src) {
            if (!src.IsMap()) return;
            if (!target.IsMap()) {
                target = YAML::Clone(src);
                return;
            }
            for (auto it = src.begin(); it != src.end(); ++it) {
                std::string k = it->first.as<std::string>();
                if (it->second.IsMap() && target[k].IsMap()) {
                    yaml_merge(target[k], it->second);
                } else {
                    target[k] = it->second;
                }
            }
        }

        static int merge_yaml(const std::string& target_file,
                              const std::string& field_value) {
            if (!util::file_exists(target_file)) return -1;

            YAML::Node existing;
            try {
                existing = YAML::LoadFile(target_file);
            } catch (...) {
                existing = YAML::Node(YAML::NodeType::Map);
            }

            YAML::Node incoming;
            try {
                incoming = YAML::Load(field_value);
            } catch (...) {
                return -1;
            }

            yaml_merge(existing, incoming);

            YAML::Emitter emitter;
            emitter << existing;
            return util::write_file(target_file,
                                    std::string(emitter.c_str()) + "\n", 0644)
                       ? 0
                       : -1;
        }

        static int merge_java_prop(const std::string& target_file,
                                   const std::string& field_value) {
            if (!util::file_exists(target_file)) return -1;

            std::map<std::string, std::string> existing_props;
            {
                std::string content = util::read_file(target_file);
                auto lines = util::split(content, '\n');
                for (const auto& line : lines) {
                    std::string trimmed = util::trim(line);
                    if (trimmed.empty() || trimmed[0] == '#') continue;
                    auto pos = trimmed.find('=');
                    if (pos != std::string::npos) {
                        existing_props[util::trim(trimmed.substr(0, pos))] =
                            util::trim(trimmed.substr(pos + 1));
                    }
                }
            }

            auto lines = util::split(field_value, '\n');
            for (const auto& line : lines) {
                std::string trimmed = util::trim(line);
                if (trimmed.empty() || trimmed[0] == '#') continue;
                auto pos = trimmed.find('=');
                if (pos != std::string::npos) {
                    existing_props[util::trim(trimmed.substr(0, pos))] =
                        util::trim(trimmed.substr(pos + 1));
                }
            }

            std::string output;
            for (const auto& kv : existing_props) {
                output += kv.first + "=" + kv.second + "\n";
            }
            return util::write_file(target_file, output, 0644) ? 0 : -1;
        }

        int config_merge(const std::string& target_file,
                         const std::string& field_value, ConfigType type) {
            switch (type) {
                case ConfigType::INI:
                    // viper does not natively support INI; Go writes the
                    // rendered value directly (creating/overwriting the target
                    // file).
                    return util::write_file(target_file, field_value, 0644)
                               ? 0
                               : -1;
                case ConfigType::JSON:
                    return merge_json(target_file, field_value);
                case ConfigType::TOML:
                    return merge_toml(target_file, field_value);
                case ConfigType::YAML:
                    return merge_yaml(target_file, field_value);
                case ConfigType::JAVA_PROP:
                    return merge_java_prop(target_file, field_value);
                default:
                    return -1;
            }
        }

    } // namespace config_merge
} // namespace inagent
