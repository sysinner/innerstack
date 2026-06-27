// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#pragma once

#include <string>

namespace inagent {
    namespace config_merge {

        enum class ConfigType {
            JSON,
            TOML,
            YAML,
            INI,
            JAVA_PROP,
            UNKNOWN,
        };

        ConfigType infer_type_from_ext(const std::string& path);
        int config_merge(const std::string& target_file,
                         const std::string& field_value, ConfigType type);

    } // namespace config_merge
} // namespace inagent
