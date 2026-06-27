// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include "log/logger.h"

#include <fcntl.h>
#include <unistd.h>

#include <cstdio>
#include <cstring>
#include <mutex>
#include <nlohmann/json.hpp>

#include "util/time_util.h"

namespace inagent {
    namespace log {

        // A plain FILE* (rather than std::ofstream) so we can reliably set
        // FD_CLOEXEC on the underlying descriptor. Without it, every forked
        // child (sh) would inherit the log fd.
        static FILE* g_log_fp = nullptr;
        static std::mutex g_log_mutex;

        static void set_cloexec(int fd) {
            int flags = fcntl(fd, F_GETFD);
            if (flags != -1) {
                fcntl(fd, F_SETFD, flags | FD_CLOEXEC);
            }
        }

        void setup(const std::string& log_file) {
            std::lock_guard<std::mutex> lock(g_log_mutex);
            if (g_log_fp) {
                std::fclose(g_log_fp);
                g_log_fp = nullptr;
            }
            // fopen "e" sets O_CLOEXEC on glibc/musl; set FD_CLOEXEC explicitly
            // as a portable fallback.
            FILE* fp = std::fopen(log_file.c_str(), "ae");
            if (!fp) {
                fp = std::fopen(log_file.c_str(), "a");
            }
            if (fp) {
                set_cloexec(fileno(fp));
            }
            g_log_fp = fp;
        }

        static void write_log(const std::string& level, const std::string& msg,
                              const std::string& key = "",
                              const std::string& value = "") {
            nlohmann::json j;
            j["time"] = util::format_time_now();
            j["level"] = level;
            j["msg"] = msg;
            if (!key.empty()) {
                j[key] = value;
            }
            std::string line = j.dump();

            std::lock_guard<std::mutex> lock(g_log_mutex);
            if (g_log_fp) {
                std::fputs(line.c_str(), g_log_fp);
                std::fputc('\n', g_log_fp);
                std::fflush(g_log_fp);
            }
            // also write to stderr for critical messages
            if (level == "ERROR") {
                std::fprintf(stderr, "%s\n", line.c_str());
            }
        }

        void info(const std::string& msg) { write_log("INFO", msg); }

        void info(const std::string& msg, const std::string& key,
                  const std::string& value) {
            write_log("INFO", msg, key, value);
        }

        void warn(const std::string& msg) { write_log("WARN", msg); }

        void warn(const std::string& msg, const std::string& key,
                  const std::string& value) {
            write_log("WARN", msg, key, value);
        }

        void error(const std::string& msg) { write_log("ERROR", msg); }

    } // namespace log
} // namespace inagent
