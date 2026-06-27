// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#pragma once

#include <cstdint>
#include <ctime>
#include <string>

namespace inagent {
    namespace task {

        struct CronSchedule {
            int second[60]; // 0 or 1
            int minute[60];
            int hour[24];
            int dom[32];   // 1-31
            int month[13]; // 1-12
            int dow[8];    // 0-6 (0 and 7 both mean Sunday, normalized to 0)

            // Whether the dom/dow field was specified as a bare "*". Used to
            // apply the Vixie cron OR/AND rule between day-of-month and
            // day-of-week.
            bool dom_star = false;
            bool dow_star = false;

            // When > 0, this schedule was produced from an "@every <duration>"
            // descriptor and fires at a fixed interval instead of via the field
            // arrays. next_fire() returns from_unix + every_seconds.
            int64_t every_seconds = 0;
        };

        class CronParser {
           public:
            static bool parse(const std::string& spec, CronSchedule& schedule);
            static int64_t next_fire(const CronSchedule& schedule,
                                     int64_t from_unix);

           private:
            static bool parse_field(const std::string& field, int* arr,
                                    int min_val, int max_val,
                                    const char* const* names = nullptr);
        };

    } // namespace task
} // namespace inagent
