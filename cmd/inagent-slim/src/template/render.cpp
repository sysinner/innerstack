// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include "template/render.h"

#include <regex>

#include "util/fs.h"
#include "util/string_util.h"

namespace inagent {
    namespace template_ {

        std::string render_with_expand(
            const std::string& text,
            const std::map<std::string, std::string>& vars) {
            static const std::regex re("\\$\\{([^}]+)\\}");
            std::string result;
            result.reserve(text.size());

            std::smatch match;
            std::string::const_iterator search_start = text.cbegin();

            while (std::regex_search(search_start, text.cend(), match, re)) {
                result.append(search_start, match[0].first);
                std::string key = match[1].str();
                auto it = vars.find(key);
                if (it != vars.end()) {
                    result.append(it->second);
                } else {
                    result.append(match[0].str());
                }
                search_start = match[0].second;
            }
            result.append(search_start, text.cend());
            return result;
        }

        int file_render(const std::string& dst, const std::string& src,
                        const std::map<std::string, std::string>& vars,
                        mode_t perm) {
            std::string content = util::read_file(src);
            if (content.empty() && !util::file_exists(src)) {
                return -1;
            }

            std::string rendered = render_with_expand(content, vars);
            if (!util::write_file(dst, rendered, perm)) {
                return -1;
            }
            return 0;
        }

    } // namespace template_
} // namespace inagent
