// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#pragma once

#include <string>

namespace inagent {
    namespace log {

        void setup(const std::string& log_file);
        void info(const std::string& msg);
        void info(const std::string& msg, const std::string& key,
                  const std::string& value);
        void warn(const std::string& msg);
        void warn(const std::string& msg, const std::string& key,
                  const std::string& value);
        void error(const std::string& msg);

    } // namespace log
} // namespace inagent

// Convenience macros with source location
#define LOG_INFO(msg) inagent::log::info(msg)
#define LOG_INFO_KV(msg, key, val) inagent::log::info(msg, key, val)
#define LOG_WARN(msg) inagent::log::warn(msg)
#define LOG_WARN_KV(msg, key, val) inagent::log::warn(msg, key, val)
#define LOG_ERROR(msg) inagent::log::error(msg)
