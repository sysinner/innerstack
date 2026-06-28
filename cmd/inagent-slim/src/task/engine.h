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

        // on_startup_aggregate reports the aggregate state of OnStartup tasks:
        // "success" when all done (or none), "running" while any pending/
        // running, "failed" if any failed. msg holds a short summary.
        void on_startup_aggregate(const model::AppReplicaInstance& app,
                                  std::string& state, std::string& msg);

    } // namespace task
} // namespace inagent
