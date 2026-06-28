// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include "daemon/daemon.h"

#include <unistd.h>

#include <algorithm>
#include <chrono>
#include <cstdlib>
#include <cstring>
#include <nlohmann/json.hpp>
#include <thread>
#include <vector>

#include "log/logger.h"
#include "signal/signal_handler.h"
#include "status/reporter.h"
#include "task/cron.h"
#include "task/engine.h"
#include "util/fs.h"
#include "util/string_util.h"
#include "util/time_util.h"

namespace inagent {
    namespace agent_daemon {

        static const std::vector<std::string> kInitDirs = {
            "/home/action/local/bin",       "/home/action/local/share",
            "/home/action/local/profile.d", "/home/action/var/tmp",
            "/home/action/var/log",         "/home/action/.ssh",
        };

        int run(const std::string& prefix) {
            // setup logging
            log::setup(prefix + "/inagent.log");

            // validate environment variables
            std::string host_id = util::trim(
                std::getenv("APP_HOST_ID") ? std::getenv("APP_HOST_ID") : "");
            if (!util::regex_match(host_id, "^[0-9a-f]{12,16}$")) {
                log::error("ENV APP_HOST_ID Not Match");
                return 1;
            }

            // APP_NAME is injected by hostlet (value = app instance Meta.Name),
            // validated as an RFC 1123 DNS label to match zonelet create-time
            // checks.
            std::string app_name = util::trim(
                std::getenv("APP_NAME") ? std::getenv("APP_NAME") : "");
            if (!util::regex_match(app_name,
                                   "^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$") ||
                app_name.size() < 3 || app_name.size() > 63) {
                log::error("ENV APP_NAME Not Match");
                return 1;
            }

            const char* rep_id_str = std::getenv("APP_REP_ID");
            if (!rep_id_str || rep_id_str[0] == '\0') {
                log::error("ENV APP_REP_ID Not Set");
                return 1;
            }
            long rep_id = std::strtol(rep_id_str, nullptr, 10);
            if (rep_id < 0 || rep_id >= 256) {
                log::error("ENV APP_REP_ID Not Valid");
                return 1;
            }
            uint32_t rep_id_u32 = static_cast<uint32_t>(rep_id);

            // create init directories
            for (const auto& dir : kInitDirs) {
                util::mkdir_all(dir, 0755);
            }

            chdir("/home/action");

            log::info("inagent daemon started");

            signal::install();

            model::AppReplicaInstance app;
            int64_t timer_duration_ms = 10;

            while (!signal::should_exit()) {
                // sleep in 1-second intervals to check signal and reap children
                int64_t elapsed = 0;
                while (elapsed < timer_duration_ms && !signal::should_exit()) {
                    int64_t sleep_ms =
                        std::min<int64_t>(1000, timer_duration_ms - elapsed);
                    std::this_thread::sleep_for(
                        std::chrono::milliseconds(sleep_ms));
                    elapsed += sleep_ms;
                    task::reap_children();
                }

                if (signal::should_exit()) break;

                // read app config
                std::string config_path =
                    prefix + "/.innerstack/app_replica.json";
                std::string content = util::read_file(config_path);
                if (content.empty()) {
                    log::error("failed to open app instance file");
                    status::set_spec_load(status::kStateFailed,
                                          "spec file empty");
                    status::flush(app.hostlet_endpoint, app_name, rep_id_u32);
                    timer_duration_ms = 10000;
                    continue;
                }

                try {
                    auto j = nlohmann::json::parse(content);
                    j.get_to(app);
                } catch (const std::exception& e) {
                    log::error(std::string("failed to decode app instance: ") +
                               e.what());
                    status::set_spec_load(status::kStateFailed,
                                          "spec decode failed");
                    status::flush(app.hostlet_endpoint, app_name, rep_id_u32);
                    timer_duration_ms = 10000;
                    continue;
                }

                // find matching replica
                app.replica = model::AppDeployReplica();
                bool found = false;
                for (const auto& rep : app.app.deploy.replicas) {
                    if (rep.host_id == host_id && rep.id == rep_id_u32) {
                        app.replica = rep;
                        found = true;
                        break;
                    }
                }
                if (!found) {
                    log::error("replica not found in app instance config");
                    status::set_spec_load(status::kStateFailed,
                                          "replica not found");
                    status::flush(app.hostlet_endpoint, app_name, rep_id_u32);
                    timer_duration_ms = 10000;
                    continue;
                }

                // align stage revision with the current deploy revision; a
                // new revision clears prior (stale) stages.
                status::set_revision(app.app.deploy.revision);
                status::set_boot();
                status::set_spec_load(status::kStateSuccess, "");

                // calculate timer duration based on cron schedules
                timer_duration_ms = 10000;
                for (const auto& t : app.app.spec.tasks) {
                    if (!t.cron.empty()) {
                        task::CronSchedule sched;
                        if (task::CronParser::parse(t.cron, sched)) {
                            int64_t next = task::CronParser::next_fire(
                                sched, util::now_unix());
                            if (next > 0) {
                                int64_t diff = next - util::now_unix();
                                if (diff > 0) {
                                    int64_t half = diff / 2;
                                    timer_duration_ms =
                                        std::max<int64_t>(1000, half * 1000);
                                }
                            }
                        }
                    }
                }

                // run tasks
                if (task::run(app) != 0) {
                    log::error("task run failed");
                }

                std::string task_state, task_msg;
                task::on_startup_aggregate(app, task_state, task_msg);
                if (!task_state.empty()) {
                    status::set_task_run(task_state, task_msg);
                }

                status::flush(app.hostlet_endpoint, app_name, rep_id_u32);
            }

            // shutdown
            task::kill_all();

            return 0;
        }

    } // namespace agent_daemon
} // namespace inagent
