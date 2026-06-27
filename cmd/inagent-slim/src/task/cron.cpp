// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include "task/cron.h"

#include <algorithm>
#include <cctype>
#include <cerrno>
#include <cstdlib>
#include <cstring>
#include <ctime>
#include <vector>

#include "util/string_util.h"

namespace inagent {
    namespace task {

        static const char* const kMonthNames[] = {
            "JAN", "FEB", "MAR", "APR", "MAY", "JUN",   "JUL",
            "AUG", "SEP", "OCT", "NOV", "DEC", nullptr,
        };

        static const char* const kDowNames[] = {
            "SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT", nullptr,
        };

        static void clear_arr(int* arr, int size) {
            memset(arr, 0, size * sizeof(int));
        }

        static std::string upper(const std::string& s) {
            std::string out = s;
            for (auto& c : out)
                c = static_cast<char>(toupper(static_cast<unsigned char>(c)));
            return out;
        }

        // Map a textual token (e.g. "JAN", "SUN") to its numeric value via a
        // name table. `base` is the value of the first entry (months: JAN=1,
        // dow: SUN=0). Returns -1 if not a name.
        static int name_lookup(const std::string& token,
                               const char* const* names, int base) {
            if (!names) return -1;
            std::string u = upper(token);
            for (int i = 0; names[i] != nullptr; ++i) {
                if (u == names[i]) return base + i;
            }
            return -1;
        }

        // Parse a single value token (no commas) which may be a name or a
        // number. Returns the numeric value, or -1 on failure. A non-numeric,
        // non-name token is rejected (rather than silently coerced to 0 via
        // atoi), matching robfig/cron which returns a parse error.
        static int parse_token(const std::string& s, const char* const* names,
                               int base) {
            if (s.empty()) return -1;
            if (names) {
                int n = name_lookup(s, names, base);
                if (n >= 0) return n;
            }
            // reject any non-digit token so "abc" does not become 0
            for (unsigned char c : s) {
                if (!std::isdigit(c)) return -1;
            }
            errno = 0;
            char* end = nullptr;
            long val = std::strtol(s.c_str(), &end, 10);
            if (errno != 0 || end == s.c_str() || *end != '\0') return -1;
            return static_cast<int>(val);
        }

        bool CronParser::parse_field(const std::string& field, int* arr,
                                     int min_val, int max_val,
                                     const char* const* names) {
            // base value for name lookup: months are 1-based (JAN=1), dow
            // 0-based (SUN=0).
            int base = (names == kMonthNames) ? 1 : 0;
            auto parts = util::split(field, ',');
            for (const auto& part : parts) {
                std::string p = util::trim(part);
                if (p.empty()) return false;

                int step = 1;
                std::string range_str = p;

                auto slash_pos = p.find('/');
                if (slash_pos != std::string::npos) {
                    range_str = p.substr(0, slash_pos);
                    step = std::atoi(p.substr(slash_pos + 1).c_str());
                    if (step <= 0) return false;
                }

                std::string r = util::trim(range_str);
                if (r.empty()) return false;

                int start, end;
                if (r == "*" || r == "?") {
                    start = min_val;
                    end = max_val;
                } else {
                    auto dash_pos = r.find('-');
                    if (dash_pos != std::string::npos) {
                        start = parse_token(r.substr(0, dash_pos), names, base);
                        end = parse_token(r.substr(dash_pos + 1), names, base);
                    } else {
                        start = parse_token(r, names, base);
                        end = start;
                    }
                }

                if (start < min_val || end > max_val || start > end ||
                    start < 0) {
                    return false;
                }

                for (int i = start; i <= end; i += step) {
                    arr[i - min_val] = 1;
                }
            }
            return true;
        }

        // Parse "@every <duration>" where duration is a sequence like 1h30m,
        // 5m, 10s. Returns seconds (>0) on success, 0 on failure.
        static int64_t parse_every(const std::string& spec) {
            const std::string prefix = "@every ";
            if (spec.size() <= prefix.size() ||
                upper(spec.substr(0, prefix.size())) != "@EVERY ") {
                return 0;
            }
            std::string dur = util::trim(spec.substr(prefix.size()));
            if (dur.empty()) return 0;

            int64_t total = 0;
            size_t i = 0;
            bool parsed_any = false;
            while (i < dur.size()) {
                // number
                size_t j = i;
                while (j < dur.size() &&
                       isdigit(static_cast<unsigned char>(dur[j])))
                    ++j;
                if (j == i) return 0; // no digits
                int64_t num = 0;
                for (size_t k = i; k < j; ++k) num = num * 10 + (dur[k] - '0');
                // unit
                if (j >= dur.size()) return 0;
                char unit = dur[j];
                int64_t mul = 0;
                switch (unit) {
                    case 'h':
                        mul = 3600;
                        break;
                    case 'm':
                        mul = 60;
                        break;
                    case 's':
                        mul = 1;
                        break;
                    default:
                        return 0;
                }
                total += num * mul;
                parsed_any = true;
                i = j + 1;
            }
            return parsed_any && total > 0 ? total : 0;
        }

        // Expand a predefined descriptor (@daily, @hourly, ...) into a 5-field
        // spec. Returns empty string if not a descriptor.
        static std::string expand_descriptor(const std::string& spec) {
            std::string u = upper(util::trim(spec));
            if (u == "@YEARLY" || u == "@ANNUALLY") return "0 0 1 1 *";
            if (u == "@MONTHLY") return "0 0 1 * *";
            if (u == "@WEEKLY") return "0 0 * * 0";
            if (u == "@DAILY" || u == "@MIDNIGHT") return "0 0 * * *";
            if (u == "@HOURLY") return "0 * * * *";
            return "";
        }

        bool CronParser::parse(const std::string& spec,
                               CronSchedule& schedule) {
            clear_arr(schedule.second, 60);
            clear_arr(schedule.minute, 60);
            clear_arr(schedule.hour, 24);
            clear_arr(schedule.dom, 32);
            clear_arr(schedule.month, 13);
            clear_arr(schedule.dow, 8);
            schedule.dom_star = false;
            schedule.dow_star = false;
            schedule.every_seconds = 0;

            if (spec.empty()) return false;

            // @every <duration>: interval-based schedule.
            if (spec.size() >= 2 && spec[0] == '@') {
                int64_t every = parse_every(spec);
                if (every > 0) {
                    schedule.every_seconds = every;
                    return true;
                }
                std::string expanded = expand_descriptor(spec);
                if (expanded.empty()) return false;
                return parse(expanded, schedule);
            }

            auto fields = util::split(spec, ' ');
            std::vector<std::string> clean_fields;
            for (const auto& f : fields) {
                if (!f.empty()) clean_fields.push_back(f);
            }

            bool has_seconds = false;
            if (clean_fields.size() == 6) {
                has_seconds = true;
            } else if (clean_fields.size() == 5) {
                schedule.second[0] = 1;
            } else {
                return false;
            }

            // Field layout: 6-field [sec min hour dom month dow],
            //               5-field [min hour dom month dow].
            int dom_idx = has_seconds ? 3 : 2;
            int dow_idx = has_seconds ? 5 : 4;

            int idx = 0;
            if (has_seconds) {
                if (!parse_field(clean_fields[idx++], schedule.second, 0, 59))
                    return false;
            }
            if (!parse_field(clean_fields[idx++], schedule.minute, 0, 59))
                return false;
            if (!parse_field(clean_fields[idx++], schedule.hour, 0, 23))
                return false;
            if (!parse_field(clean_fields[idx++], schedule.dom, 1, 31))
                return false;
            if (!parse_field(clean_fields[idx++], schedule.month, 1, 12,
                             kMonthNames))
                return false;

            // A bare "*" (or "?") marks a field as unrestricted for the Vixie
            // OR/AND rule between day-of-month and day-of-week. "*/2" is
            // restricted.
            const std::string& dom_field = clean_fields[dom_idx];
            const std::string& dow_field = clean_fields[dow_idx];
            {
                std::string dt = util::trim(dom_field);
                schedule.dom_star = (dt == "*" || dt == "?");
            }
            {
                std::string wt = util::trim(dow_field);
                schedule.dow_star = (wt == "*" || wt == "?");
            }

            // day-of-week: 0-7 where 0 and 7 both mean Sunday. Parse into a
            // temp array sized 8, then normalize 7 -> 0.
            int dow_tmp[8] = {0, 0, 0, 0, 0, 0, 0, 0};
            if (!parse_field(dow_field, dow_tmp, 0, 7, kDowNames)) return false;
            for (int i = 0; i < 8; ++i) {
                if (dow_tmp[i]) {
                    schedule.dow[i % 7] = 1;
                }
            }

            return true;
        }

        int64_t CronParser::next_fire(const CronSchedule& schedule,
                                      int64_t from_unix) {
            // Interval-based schedule (@every).
            if (schedule.every_seconds > 0) {
                return from_unix + schedule.every_seconds;
            }

            time_t t = static_cast<time_t>(from_unix);
            struct tm tm_buf;
            localtime_r(&t, &tm_buf);

            // start from next second
            tm_buf.tm_sec++;
            if (tm_buf.tm_sec >= 60) {
                tm_buf.tm_sec = 0;
                tm_buf.tm_min++;
            }
            mktime(&tm_buf);

            // search up to 5 years ahead
            for (int iter = 0; iter < 5 * 366 * 24 * 3600; iter++) {
                if (!schedule.month[tm_buf.tm_mon]) {
                    tm_buf.tm_mday = 1;
                    tm_buf.tm_hour = 0;
                    tm_buf.tm_min = 0;
                    tm_buf.tm_sec = 0;
                    tm_buf.tm_mon++;
                    mktime(&tm_buf);
                    continue;
                }

                // Vixie rule: when both dom and dow are restricted, fire if
                // either matches; if only one is restricted, that one decides;
                // if both are unrestricted, any day matches.
                //
                // dom is parsed with min_val=1 so day D is stored at dom[D-1];
                // tm_mday is 1-based, hence the index adjustment. (month needs
                // no adjustment because tm_mon is 0-based.)
                int dow = tm_buf.tm_wday;
                bool dom_match = schedule.dom[tm_buf.tm_mday - 1];
                bool dow_match = schedule.dow[dow];
                bool day_match;
                if (schedule.dom_star && schedule.dow_star) {
                    day_match = true;
                } else if (schedule.dom_star) {
                    day_match = dow_match;
                } else if (schedule.dow_star) {
                    day_match = dom_match;
                } else {
                    day_match = dom_match || dow_match;
                }
                if (!day_match) {
                    tm_buf.tm_mday++;
                    tm_buf.tm_hour = 0;
                    tm_buf.tm_min = 0;
                    tm_buf.tm_sec = 0;
                    mktime(&tm_buf);
                    continue;
                }

                if (!schedule.hour[tm_buf.tm_hour]) {
                    tm_buf.tm_hour++;
                    tm_buf.tm_min = 0;
                    tm_buf.tm_sec = 0;
                    mktime(&tm_buf);
                    continue;
                }

                if (!schedule.minute[tm_buf.tm_min]) {
                    tm_buf.tm_min++;
                    tm_buf.tm_sec = 0;
                    mktime(&tm_buf);
                    continue;
                }

                if (!schedule.second[tm_buf.tm_sec]) {
                    tm_buf.tm_sec++;
                    mktime(&tm_buf);
                    continue;
                }

                return static_cast<int64_t>(mktime(&tm_buf));
            }

            return -1; // no match found
        }

    } // namespace task
} // namespace inagent
