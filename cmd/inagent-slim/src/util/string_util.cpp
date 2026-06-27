// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include "util/string_util.h"

#include <algorithm>
#include <cctype>
#include <cstdarg>
#include <cstdio>
#include <cstring>
#include <regex>

namespace inagent {
    namespace util {

        std::string trim(const std::string& s) {
            auto start = s.begin();
            while (start != s.end() &&
                   std::isspace(static_cast<unsigned char>(*start))) {
                ++start;
            }
            auto end = s.end();
            while (end != start &&
                   std::isspace(static_cast<unsigned char>(*(end - 1)))) {
                --end;
            }
            return std::string(start, end);
        }

        std::string trim_left(const std::string& s) {
            auto start = s.begin();
            while (start != s.end() &&
                   std::isspace(static_cast<unsigned char>(*start))) {
                ++start;
            }
            return std::string(start, s.end());
        }

        std::vector<std::string> split(const std::string& s, char delim) {
            std::vector<std::string> parts;
            std::string::size_type start = 0, pos;
            while ((pos = s.find(delim, start)) != std::string::npos) {
                parts.emplace_back(s.substr(start, pos - start));
                start = pos + 1;
            }
            parts.emplace_back(s.substr(start));
            return parts;
        }

        std::string replace_all(const std::string& s, const std::string& from,
                                const std::string& to) {
            if (from.empty()) return s;
            std::string result;
            result.reserve(s.size());
            std::string::size_type start = 0, pos;
            while ((pos = s.find(from, start)) != std::string::npos) {
                result.append(s, start, pos - start);
                result.append(to);
                start = pos + from.size();
            }
            result.append(s, start, std::string::npos);
            return result;
        }

        bool regex_match(const std::string& s, const std::string& pattern) {
            try {
                return std::regex_match(s, std::regex(pattern));
            } catch (...) {
                return false;
            }
        }

        std::string fmt_sprintf(const char* fmt, ...) {
            va_list args;
            va_start(args, fmt);
            va_list args2;
            va_copy(args2, args);
            int len = vsnprintf(nullptr, 0, fmt, args);
            va_end(args);
            if (len < 0) {
                va_end(args2);
                return "";
            }
            std::string out(static_cast<size_t>(len), '\0');
            vsnprintf(&out[0], static_cast<size_t>(len) + 1, fmt, args2);
            va_end(args2);
            return out;
        }

    } // namespace util
} // namespace inagent
