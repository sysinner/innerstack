// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#pragma once

#include <sys/types.h>

#include <cstdint>
#include <map>
#include <string>
#include <vector>

#include "model/types.h"

namespace inagent {
    namespace task {

        int run(const model::AppReplicaInstance& app);
        int kill_all();
        void reap_children();

    } // namespace task
} // namespace inagent
