// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#pragma once

#include <cstdint>
#include <string>

#include "model/types.h"

namespace inagent {
    namespace status {

        // Stage state constants (mirror pkg/inapi const.go).
        extern const char* const kStatePending;
        extern const char* const kStateRunning;
        extern const char* const kStateSuccess;
        extern const char* const kStateFailed;

        // Stage name constants.
        extern const char* const kStageInagentBoot;
        extern const char* const kStageSpecLoad;
        extern const char* const kStageTaskRun;

        // set_revision aligns the reporter with the current AppDeploy.revision.
        // When the revision changes, prior stages are cleared (they were based
        // on stale meta).
        void set_revision(uint64_t rev);

        // set_boot records inagent daemon startup as an instantaneous stage.
        void set_boot();

        // set_spec_load records the app_replica.json load result.
        void set_spec_load(const std::string& state, const std::string& msg);

        // set_task_run records the aggregate OnStartup task execution state.
        void set_task_run(const std::string& state, const std::string& msg);

        // flush POSTs the current stage tree to the hostlet status API when
        // there are unsent changes or the heartbeat interval has elapsed. On
        // success it clears the dirty flag. A missing endpoint is a no-op.
        void flush(const model::HostletStatusEndpoint& endpoint,
                   const std::string& instance_name, uint32_t rep_id);

    } // namespace status
} // namespace inagent
