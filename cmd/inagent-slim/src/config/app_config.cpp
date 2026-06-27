// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include "config/app_config.h"

#include <nlohmann/json.hpp>

#include "template/var_params.h"
#include "util/fs.h"
#include "util/string_util.h"

namespace inagent {
    namespace config {

        AppConfigHelper::AppConfigHelper(const std::string& prefix)
            : prefix_(prefix) {}

        bool AppConfigHelper::load() {
            std::string path = prefix_ + "/.innerstack/app_replica.json";
            std::string content = util::read_file(path);
            if (content.empty()) return false;

            nlohmann::json j;
            try {
                j = nlohmann::json::parse(content);
                j.get_to(app_);
            } catch (...) {
                return false;
            }

            // Mirror Go NewAppConfigHelper, which rejects when App/Spec/Deploy/
            // Replica are nil. A protobuf nil field is absent from the JSON, so
            // check presence of the corresponding objects instead of requiring
            // specific sub-fields to be non-empty.
            if (!j.contains("app") || !j["app"].contains("spec") ||
                !j["app"].contains("deploy") || !j.contains("replica")) {
                return false;
            }

            updated_ms_ = util::file_mtime_ms(path);
            return true;
        }

        const model::AppDeployConfigItem* AppConfigHelper::config(
            const std::string& name) const {
            auto parts = util::split(name, '.');
            if (parts.empty()) return nullptr;

            for (const auto& item : app_.app.deploy.configs) {
                if (item.name == parts[0]) {
                    if (parts.size() > 1) {
                        for (const auto& sub : item.items) {
                            if (sub.name == parts[1]) return &sub;
                        }
                        return nullptr;
                    }
                    return &item;
                }
            }
            return nullptr;
        }

        std::string AppConfigHelper::config_value(
            const std::string& name) const {
            auto* item = config(name);
            return item ? item->value : "";
        }

        std::map<std::string, std::string> AppConfigHelper::params() const {
            return template_::var_params(app_);
        }

    } // namespace config
} // namespace inagent
