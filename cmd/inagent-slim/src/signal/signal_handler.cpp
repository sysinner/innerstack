// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include "signal/signal_handler.h"

#include <csignal>
#include <cstring>

namespace inagent {
    namespace signal {

        static std::atomic<bool> g_exit_flag{false};

        static void handler(int /*sig*/) {
            // Only async-signal-safe operations here. std::atomic<bool>::store
            // with a constant is safe; do NOT touch std::condition_variable or
            // any non-async-signal-safe facility. The daemon loop polls
            // should_exit() every second, so no signaling primitive is needed.
            g_exit_flag.store(true);
        }

        void install() {
            struct sigaction sa;
            memset(&sa, 0, sizeof(sa));
            sa.sa_handler = handler;
            sigemptyset(&sa.sa_mask);
            sa.sa_flags = 0;
            sigaction(SIGTERM, &sa, nullptr);
            sigaction(SIGINT, &sa, nullptr);

            // Ignore SIGPIPE: writing to a child's stdin pipe after the child
            // has exited early (or failed to exec) must yield EPIPE from
            // write() rather than terminating inagent, which runs as PID 1
            // inside the container.
            struct sigaction sp;
            memset(&sp, 0, sizeof(sp));
            sp.sa_handler = SIG_IGN;
            sigemptyset(&sp.sa_mask);
            sp.sa_flags = 0;
            sigaction(SIGPIPE, &sp, nullptr);
        }

        bool should_exit() { return g_exit_flag.load(); }

    } // namespace signal
} // namespace inagent
