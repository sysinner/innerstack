// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#pragma once

#include <sys/types.h>

#include <atomic>
#include <cstdint>
#include <memory>
#include <string>

#include "model/types.h"

namespace inagent {
    namespace task {

        // PipeReader drains a child's stdout/stderr pipe on a background thread
        // so the child can never block on a full pipe (which would deadlock the
        // parent when the parent only reads after waitpid()). The buffer is
        // filled by the reader thread and consumed by the scheduler thread
        // after the child exits.
        struct PipeReader {
            std::string buf;
            std::atomic<bool> done{false};
        };

        struct ExecutorStatus {
            int64_t updated = 0;
            std::string state;
            int64_t exec_window = 0;
            int64_t done_updated = 0;
            int64_t fail_updated = 0;
            std::string fail_message;
            int64_t on_failed_retry_num = 0;
            std::string output;
            pid_t child_pid = 0;
            int stdout_fd = -1;
            bool child_exited = false;
            std::shared_ptr<PipeReader> reader;
        };

    } // namespace task
} // namespace inagent
