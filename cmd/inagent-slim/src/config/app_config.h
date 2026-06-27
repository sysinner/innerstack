// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#pragma once

#include <map>
#include <string>

#include "model/types.h"

namespace inagent {
    namespace config {

        class AppConfigHelper {
           public:
            explicit AppConfigHelper(const std::string& prefix);

            bool load();
            const model::AppReplicaInstance& app() const { return app_; }
            const model::AppSpec& spec() const { return app_.app.spec; }

            const model::AppDeployConfigItem* config(
                const std::string& name) const;
            std::string config_value(const std::string& name) const;
            std::map<std::string, std::string> params() const;

           private:
            model::AppReplicaInstance app_;
            std::string prefix_;
            int64_t updated_ms_ = 0;
        };

    } // namespace config
} // namespace inagent
