// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#pragma once

#include <sys/types.h>

#include <map>
#include <string>

namespace inagent {
    namespace template_ {

        std::string render_with_expand(
            const std::string& text,
            const std::map<std::string, std::string>& vars);

        int file_render(const std::string& dst, const std::string& src,
                        const std::map<std::string, std::string>& vars,
                        mode_t perm = 0640);

    } // namespace template_
} // namespace inagent
