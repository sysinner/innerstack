// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include "util/time_util.h"

#include <chrono>
#include <cstdio>
#include <ctime>

namespace inagent {
    namespace util {

        int64_t now_unix() {
            return std::chrono::duration_cast<std::chrono::seconds>(
                       std::chrono::system_clock::now().time_since_epoch())
                .count();
        }

        int64_t now_unix_ms() {
            return std::chrono::duration_cast<std::chrono::milliseconds>(
                       std::chrono::system_clock::now().time_since_epoch())
                .count();
        }

        std::string format_time(int64_t unix_ms) {
            time_t t = static_cast<time_t>(unix_ms / 1000);
            int ms = static_cast<int>(unix_ms % 1000);
            struct tm tm_buf;
            localtime_r(&t, &tm_buf);
            char buf[32];
            snprintf(buf, sizeof(buf), "%04d-%02d-%02d %02d:%02d:%02d.%03d",
                     tm_buf.tm_year + 1900, tm_buf.tm_mon + 1, tm_buf.tm_mday,
                     tm_buf.tm_hour, tm_buf.tm_min, tm_buf.tm_sec, ms);
            return std::string(buf);
        }

        std::string format_time_now() { return format_time(now_unix_ms()); }

    } // namespace util
} // namespace inagent
