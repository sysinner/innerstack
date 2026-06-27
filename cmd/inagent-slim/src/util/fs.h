// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#pragma once

#include <sys/types.h>

#include <cstdint>
#include <string>

namespace inagent {
    namespace util {

        bool mkdir_all(const std::string& path, mode_t mode = 0755);
        std::string read_file(const std::string& path);
        bool write_file(const std::string& path, const std::string& content,
                        mode_t mode = 0644);
        bool file_exists(const std::string& path);
        int64_t file_mtime_ms(const std::string& path);

    } // namespace util
} // namespace inagent
