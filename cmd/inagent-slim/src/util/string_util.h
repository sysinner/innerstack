// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#pragma once

#include <cstdint>
#include <string>
#include <vector>

namespace inagent {
    namespace util {

        std::string trim(const std::string& s);
        std::string trim_left(const std::string& s);
        std::vector<std::string> split(const std::string& s, char delim);
        std::string replace_all(const std::string& s, const std::string& from,
                                const std::string& to);
        bool regex_match(const std::string& s, const std::string& pattern);
        std::string fmt_sprintf(const char* fmt, ...);

    } // namespace util
} // namespace inagent
