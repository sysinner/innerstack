// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include "config_merge/merger.h"

#include <yaml-cpp/yaml.h>

#include <map>
#include <nlohmann/json.hpp>
#include <set>
#include <string>
#include <toml.hpp>
#include <vector>

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

        // --- INI merge (configparser-style, dependency-free) ---
        // Merged at the section/key level so the base config produced by
        // `config-render` is preserved: the override wins on key conflicts and
        // new keys/sections are appended. Mirrors the Go inagent's go-ini based
        // merge. Comments and blank lines in the base are preserved; override
        // comment/blank lines contribute no keys (so a comment-only override is
        // a no-op and leaves the base intact).

        struct IniSection {
            std::string name;   // bracketed name, "" = preamble
            std::string header; // raw "[name]" line, "" for the preamble
            std::vector<std::string> lines;
        };

        static bool is_ini_section_header(const std::string& raw) {
            std::string t = util::trim(raw);
            return t.size() >= 2 && t.front() == '[';
        }

        static std::string parse_ini_section_name(const std::string& raw) {
            std::string t = util::trim(raw);
            if (t.size() < 2 || t.front() != '[') return "";
            std::string::size_type end = t.find(']');
            return util::trim(end == std::string::npos ? t.substr(1)
                                                       : t.substr(1, end - 1));
        }

        // Fills key/value when raw is a "key = value" entry (first '=' splits;
        // both sides trimmed). Section headers, comments and blank lines return
        // false.
        static bool parse_ini_kv(const std::string& raw, std::string& key,
                                 std::string& value) {
            std::string t = util::trim(raw);
            if (t.empty()) return false;
            char c = t.front();
            if (c == ';' || c == '#' || c == '[') return false;
            std::string::size_type pos = t.find('=');
            if (pos == std::string::npos) return false;
            key = util::trim(t.substr(0, pos));
            value = util::trim(t.substr(pos + 1));
            return !key.empty();
        }

        static std::vector<IniSection> parse_ini(const std::string& content) {
            std::vector<IniSection> secs;
            secs.push_back({"", "", {}}); // preamble: lines before first [section]
            for (const auto& raw : util::split(content, '\n')) {
                if (is_ini_section_header(raw)) {
                    secs.push_back({parse_ini_section_name(raw), raw, {}});
                } else {
                    secs.back().lines.push_back(raw);
                }
            }
            return secs;
        }

        static int merge_ini(const std::string& target_file,
                             const std::string& field_value) {
            std::vector<IniSection> secs;
            if (util::file_exists(target_file)) {
                secs = parse_ini(util::read_file(target_file));
            }
            if (secs.empty()) {
                // Base missing/unreadable (config-render not run): start empty
                // so the override becomes the whole file.
                secs.push_back({"", "", {}});
            }

            // Parse the override into section -> (key -> value), tracking the
            // first-seen order of sections that actually contribute keys.
            std::map<std::string, std::map<std::string, std::string>> ov;
            std::vector<std::string> ov_order;
            std::string cur; // current override section, "" until a header
            for (const auto& raw : util::split(field_value, '\n')) {
                if (is_ini_section_header(raw)) {
                    cur = parse_ini_section_name(raw);
                    continue;
                }
                std::string k, v;
                if (!parse_ini_kv(raw, k, v)) continue; // comment/blank/non-kv
                if (ov.find(cur) == ov.end()) ov_order.push_back(cur);
                ov[cur][k] = v;
            }

            // Apply overrides in place: override value wins, base order and
            // comment lines are kept.
            std::map<std::string, std::set<std::string>> used;
            for (auto& sec : secs) {
                auto sit = ov.find(sec.name);
                if (sit == ov.end()) continue;
                for (auto& line : sec.lines) {
                    std::string k, v;
                    if (!parse_ini_kv(line, k, v)) continue;
                    auto kit = sit->second.find(k);
                    if (kit == sit->second.end()) continue;
                    line = k + " = " + kit->second;
                    used[sec.name].insert(k);
                }
            }

            // Append override keys absent from the base, into their section.
            for (const auto& sname : ov_order) {
                IniSection* sec = nullptr;
                for (auto& s : secs) {
                    if (s.name == sname) {
                        sec = &s;
                        break;
                    }
                }
                if (sec == nullptr) {
                    IniSection ns;
                    ns.name = sname;
                    ns.header = sname.empty() ? "" : ("[" + sname + "]");
                    secs.push_back(std::move(ns));
                    sec = &secs.back();
                }
                for (const auto& kv : ov[sname]) {
                    if (used[sname].count(kv.first)) continue;
                    sec->lines.push_back(kv.first + " = " + kv.second);
                }
            }

            std::string out;
            for (const auto& sec : secs) {
                if (!sec.header.empty()) out += sec.header + "\n";
                for (const auto& line : sec.lines) out += line + "\n";
            }
            return util::write_file(target_file, out, 0644) ? 0 : -1;
        }

        int config_merge(const std::string& target_file,
                         const std::string& field_value, ConfigType type) {
            switch (type) {
                case ConfigType::INI:
                    return merge_ini(target_file, field_value);
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
