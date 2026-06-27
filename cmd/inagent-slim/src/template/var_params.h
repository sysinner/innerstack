// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#pragma once

#include <map>
#include <string>

#include "model/types.h"

namespace inagent {
    namespace template_ {

        std::map<std::string, std::string> var_params(
            const model::AppReplicaInstance& app);

    } // namespace template_
} // namespace inagent
