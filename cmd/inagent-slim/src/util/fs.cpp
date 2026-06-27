// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include "util/fs.h"

#include <sys/stat.h>

#include <cerrno>
#include <cstring>
#include <fstream>
#include <sstream>
#include <string>

namespace inagent {
    namespace util {

        bool mkdir_all(const std::string& path, mode_t mode) {
            if (path.empty()) return false;
            size_t pos = 0;
            while ((pos = path.find('/', pos + 1)) != std::string::npos) {
                std::string sub = path.substr(0, pos);
                if (sub.empty()) continue;
                if (mkdir(sub.c_str(), mode) != 0 && errno != EEXIST) {
                    return false;
                }
            }
            if (mkdir(path.c_str(), mode) != 0 && errno != EEXIST) {
                return false;
            }
            return true;
        }

        std::string read_file(const std::string& path) {
            std::ifstream ifs(path, std::ios::in | std::ios::binary);
            if (!ifs.is_open()) return "";
            std::ostringstream ss;
            ss << ifs.rdbuf();
            return ss.str();
        }

        bool write_file(const std::string& path, const std::string& content,
                        mode_t mode) {
            std::ofstream ofs(
                path, std::ios::out | std::ios::trunc | std::ios::binary);
            if (!ofs.is_open()) return false;
            ofs.write(content.data(), content.size());
            ofs.close();
            chmod(path.c_str(), mode);
            return true;
        }

        bool file_exists(const std::string& path) {
            struct stat st;
            return stat(path.c_str(), &st) == 0;
        }

        int64_t file_mtime_ms(const std::string& path) {
            struct stat st;
            if (stat(path.c_str(), &st) != 0) return 0;
#ifdef __APPLE__
            return static_cast<int64_t>(st.st_mtimespec.tv_sec) * 1000 +
                   st.st_mtimespec.tv_nsec / 1000000;
#else
            return static_cast<int64_t>(st.st_mtim.tv_sec) * 1000 +
                   st.st_mtim.tv_nsec / 1000000;
#endif
        }

    } // namespace util
} // namespace inagent
