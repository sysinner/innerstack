// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#pragma once

#include <cstdint>
#include <string>

namespace inagent {
    namespace util {

        int64_t now_unix();
        int64_t now_unix_ms();
        std::string format_time(int64_t unix_ms);
        std::string format_time_now();

    } // namespace util
} // namespace inagent
