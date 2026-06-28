// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include "task/engine.h"

#include <grp.h>
#include <sys/wait.h>
#include <unistd.h>

#include <algorithm>
#include <cerrno>
#include <chrono>
#include <csignal>
#include <cstring>
#include <memory>
#include <thread>

#include "log/logger.h"
#include "task/cron.h"
#include "task/executor.h"
#include "template/render.h"
#include "template/var_params.h"
#include "util/string_util.h"
#include "util/time_util.h"

namespace inagent {
    namespace task {

        static const int64_t kMinRetryIntervalSeconds = 10;
        static const int kDefaultUserID = 2048;
        static const int kDefaultGroupID = 2048;

        static model::AppReplicaInstance g_app;
        static std::map<std::string, ExecutorStatus> g_exec_statuses;

        // Write all bytes, retrying on EINTR. Returns false on a hard error
        // such as EPIPE (the child closed stdin before reading everything).
        static bool write_all(int fd, const std::string& data) {
            size_t off = 0;
            while (off < data.size()) {
                ssize_t n = ::write(fd, data.data() + off, data.size() - off);
                if (n < 0) {
                    if (errno == EINTR) continue;
                    return false; // EPIPE, EBADF, ...
                }
                if (n == 0) return false;
                off += static_cast<size_t>(n);
            }
            return true;
        }

        // Start a background reader that drains fd into pr->buf until EOF, then
        // sets pr->done. The thread is detached; pr is kept alive by the
        // shared_ptr stored in the ExecutorStatus (and captured here).
        static void start_reader(const std::shared_ptr<PipeReader>& pr,
                                 int fd) {
            std::thread([pr, fd]() {
                char tmp[8192];
                ssize_t n;
                while ((n = read(fd, tmp, sizeof(tmp))) > 0) {
                    pr->buf.append(tmp, static_cast<size_t>(n));
                }
                close(fd);
                pr->done.store(true);
            }).detach();
        }

        // Wait (bounded) for the reader thread to finish draining, then return
        // the captured output.
        static std::string finish_reader(ExecutorStatus& es) {
            if (es.reader) {
                int64_t deadline = util::now_unix_ms() + 3000;
                while (!es.reader->done.load() &&
                       util::now_unix_ms() < deadline) {
                    std::this_thread::sleep_for(std::chrono::milliseconds(5));
                }
                std::string out = util::trim(es.reader->buf);
                es.reader.reset();
                return out;
            }
            return "";
        }

        // Apply a reaped child's exit status to its executor state. Centralized
        // so the run() pre-check, async re-check and reap_children() stay
        // consistent.
        static void apply_exit(ExecutorStatus& es, int status,
                               const std::string& name) {
            std::string output = finish_reader(es);
            if (WIFEXITED(status) && WEXITSTATUS(status) == 0) {
                es.done_updated = std::max(util::now_unix(), es.exec_window);
                es.fail_updated = 0;
                es.state = "exited";
                log::info(util::fmt_sprintf("task [%s] success", name.c_str()));
            } else {
                es.output = output;
                if (!output.empty()) {
                    es.fail_message = util::fmt_sprintf(
                        "process error, output: %s", output.c_str());
                } else {
                    es.fail_message = "process error";
                }
                es.done_updated = 0;
                es.fail_updated = std::max(util::now_unix(), es.exec_window);
                log::error(util::fmt_sprintf("task [%s] failed, err %s",
                                             name.c_str(),
                                             es.fail_message.c_str()));
            }
            es.child_pid = 0;
            es.stdout_fd = -1;
            es.updated = util::now_unix();
        }

        static bool depend_allow(const model::AppSpecTask& task) {
            for (const auto& name : task.depends_on) {
                auto it = g_exec_statuses.find(name);
                if (it == g_exec_statuses.end() ||
                    it->second.done_updated < it->second.exec_window) {
                    return false;
                }
            }
            return true;
        }

        static void child_exec(const std::string& working_dir,
                               const std::string& run_user, int stdout_fd) {
            // redirect stdout/stderr
            dup2(stdout_fd, STDOUT_FILENO);
            dup2(stdout_fd, STDERR_FILENO);
            close(stdout_fd);

            // switch credentials. Only "root" and "action" are supported; any
            // other value falls back to the default "action" uid/gid (warned by
            // the caller).
            uid_t uid = kDefaultUserID;
            gid_t gid = kDefaultGroupID;
            if (run_user == "root") {
                uid = 0;
                gid = 0;
            }

            if (gid != 0) {
                // drop supplementary groups before switching gid/uid so the
                // child does not retain the parent's (root) group membership.
                gid_t gid_list[1] = {gid};
                setgroups(1, gid_list);
                setgid(gid);
            }
            if (uid != 0) {
                setuid(uid);
            }

            // change directory. On failure, exit the same way an exec failure
            // would (matches Go, where cmd.Start returns an error if Dir does
            // not exist) so the parent observes a failed task rather than a
            // child silently running in the wrong directory.
            if (chdir(working_dir.c_str()) != 0) {
                _exit(127);
            }

            execlp("sh", "sh", nullptr);
            _exit(127);
        }

        static pid_t spawn_process(const std::string& script,
                                   const std::string& working_dir,
                                   const std::string& run_user,
                                   int& stdout_pipe_read,
                                   bool with_pipefail = true) {
            int stdin_pipe[2], stdout_pipe[2];
            if (pipe(stdin_pipe) != 0) return -1;
            if (pipe(stdout_pipe) != 0) {
                close(stdin_pipe[0]);
                close(stdin_pipe[1]);
                return -1;
            }

            // build preamble
            std::string preamble = "set -e\n";
            if (with_pipefail) {
                preamble += "set -o pipefail\n";
            }
            std::string full_script = preamble + script + "\nexit\n";

            // Resolve the working directory BEFORE fork. The child must only
            // call async-signal-safe functions between fork and exec; any
            // std::string allocation in the child could deadlock on the malloc
            // lock held by another thread at fork time. child_exec takes this
            // by const reference, so no allocation happens post-fork.
            std::string dir =
                working_dir.empty() ? "/home/action" : working_dir;

            pid_t pid = fork();
            if (pid < 0) {
                close(stdin_pipe[0]);
                close(stdin_pipe[1]);
                close(stdout_pipe[0]);
                close(stdout_pipe[1]);
                return -1;
            }

            if (pid == 0) {
                // child
                close(stdin_pipe[1]);
                close(stdout_pipe[0]);
                dup2(stdin_pipe[0], STDIN_FILENO);
                close(stdin_pipe[0]);

                child_exec(dir, run_user, stdout_pipe[1]);
                _exit(127);
            }

            // parent: write script to child's stdin, then close. SIGPIPE is
            // ignored (see signal::install) so a child that exits early yields
            // EPIPE here rather than killing inagent.
            close(stdin_pipe[0]);
            close(stdout_pipe[1]);
            stdout_pipe_read = stdout_pipe[0];

            write_all(stdin_pipe[1], full_script);
            close(stdin_pipe[1]);

            return pid;
        }

        static int task_sync_action(
            const model::AppSpecTask& task,
            const std::map<std::string, std::string>& params) {
            std::string script = util::trim(task.script);
            if (script.empty()) return 0;

            script = template_::render_with_expand(script, params);

            std::string working_dir =
                task.working_dir.empty() ? "/home/action" : task.working_dir;

            // sync action: no pipefail (matches Go behavior)
            int stdout_fd = -1;
            pid_t pid = spawn_process(script, working_dir, task.run_user,
                                      stdout_fd, false);
            if (pid <= 0) return -1;

            // drain output concurrently so the child cannot block on a full
            // pipe.
            auto reader = std::make_shared<PipeReader>();
            start_reader(reader, stdout_fd);

            // wait with 5 second timeout
            int64_t deadline = util::now_unix_ms() + 5000;
            bool exited = false;
            int status = 0;
            while (util::now_unix_ms() < deadline) {
                pid_t result = waitpid(pid, &status, WNOHANG);
                if (result > 0) {
                    exited = true;
                    break;
                }
                std::this_thread::sleep_for(std::chrono::milliseconds(10));
            }

            if (exited) {
                // wait for the reader to finish draining, then log.
                int64_t rd = util::now_unix_ms() + 3000;
                while (!reader->done.load() && util::now_unix_ms() < rd) {
                    std::this_thread::sleep_for(std::chrono::milliseconds(5));
                }
                std::string output = util::trim(reader->buf);
                if (WIFEXITED(status) && WEXITSTATUS(status) == 0) {
                    log::info(util::fmt_sprintf("task [%s] success",
                                                task.name.c_str()));
                } else {
                    log::error(util::fmt_sprintf("task [%s] failed, err %s",
                                                 task.name.c_str(),
                                                 output.c_str()));
                }
                return 0;
            }

            // timeout: kill, then reap with a bounded wait (the child may be in
            // an uninterruptible state where SIGKILL does not take effect
            // immediately).
            kill(pid, SIGKILL);
            int64_t reap_deadline = util::now_unix_ms() + 3000;
            while (util::now_unix_ms() < reap_deadline) {
                if (waitpid(pid, &status, WNOHANG) > 0) break;
                std::this_thread::sleep_for(std::chrono::milliseconds(10));
            }
            // best-effort final blocking reap only within remaining budget;
            // avoid hanging forever on a D-state child.
            if (waitpid(pid, &status, WNOHANG) <= 0) {
                // leave it to the orphan reaper; do not block PID 1
                // indefinitely.
                log::warn(util::fmt_sprintf("task [%s] kill reap pending",
                                            task.name.c_str()));
            }
            return 0;
        }

        static int task_async_action(
            const model::AppSpecTask& task, ExecutorStatus& es,
            const std::map<std::string, std::string>& params) {
            if (es.done_updated >= es.exec_window) return 0;

            int64_t tn = util::now_unix();

            if (es.child_pid > 0) {
                // check if child has exited
                int status;
                pid_t result = waitpid(es.child_pid, &status, WNOHANG);
                if (result > 0) {
                    es.child_exited = true;
                    apply_exit(es, status, task.name);
                } else {
                    // still running
                    if (es.updated + 60 < tn) {
                        int64_t duration = tn - es.exec_window;
                        log::info(util::fmt_sprintf(
                            "task [%s] running, duration %lds",
                            task.name.c_str(), static_cast<long>(duration)));
                        es.updated = tn;
                    }
                }
                return 0;
            }

            std::string script = util::trim(task.script);
            if (script.empty()) return 0;

            es.state = "running";
            es.updated = tn;

            script = template_::render_with_expand(script, params);

            log::info(util::fmt_sprintf(
                "inagent/exec run, name: %s, user: %s, script: %s",
                task.name.c_str(), task.run_user.c_str(), script.c_str()));

            // warn on invalid user
            if (!task.run_user.empty() && task.run_user != "root" &&
                task.run_user != "action") {
                log::warn(util::fmt_sprintf(
                    "task [%s] user invalid (%s), using default 'action'",
                    task.name.c_str(), task.run_user.c_str()));
            }

            std::string working_dir =
                task.working_dir.empty() ? "/home/action" : task.working_dir;

            int stdout_fd = -1;
            pid_t pid = spawn_process(script, working_dir, task.run_user,
                                      stdout_fd, true);
            if (pid <= 0) {
                log::error(util::fmt_sprintf("task [%s] CMD failed",
                                             task.name.c_str()));
                return -1;
            }

            es.child_pid = pid;
            es.stdout_fd = stdout_fd;
            es.reader = std::make_shared<PipeReader>();
            start_reader(es.reader, stdout_fd);

            return 0;
        }

        int run(const model::AppReplicaInstance& app) {
            if (app.app.spec.tasks.empty()) return 0;

            g_app = app;

            std::map<std::string, std::string> params;
            int64_t tn = util::now_unix();

            for (const auto& task : app.app.spec.tasks) {
                if (task.on_shutdown) continue;

                auto it = g_exec_statuses.find(task.name);
                if (it == g_exec_statuses.end()) {
                    ExecutorStatus es;
                    es.updated = tn;
                    if (task.on_startup || task.interval_seconds > 0) {
                        es.exec_window = tn;
                    }
                    g_exec_statuses[task.name] = es;
                    it = g_exec_statuses.find(task.name);
                }

                ExecutorStatus& es = it->second;

                // First check if any running child has exited
                if (es.child_pid > 0) {
                    int status;
                    pid_t result = waitpid(es.child_pid, &status, WNOHANG);
                    if (result > 0) {
                        es.child_exited = true;
                        apply_exit(es, status, task.name);
                    }
                }

                if (task.on_startup) {
                    if (es.exec_window > 0 && es.done_updated == 0 &&
                        es.fail_updated >= es.exec_window) {
                        es.exec_window = std::max(
                            es.exec_window + kMinRetryIntervalSeconds, tn);
                    }
                } else if (task.interval_seconds > 0) {
                    int64_t last_updated =
                        std::max(es.done_updated, es.fail_updated);
                    if (last_updated + task.interval_seconds <= tn) {
                        es.exec_window = std::max(
                            es.exec_window + kMinRetryIntervalSeconds, tn);
                    }
                } else if (!task.cron.empty()) {
                    int64_t last_updated =
                        std::max(es.done_updated, es.fail_updated);
                    if (es.exec_window == 0 || es.exec_window <= last_updated) {
                        CronSchedule sched;
                        if (CronParser::parse(task.cron, sched)) {
                            int64_t next =
                                CronParser::next_fire(sched, util::now_unix());
                            if (next > 0) {
                                es.exec_window = std::max(next, last_updated);
                            }
                        } else {
                            log::warn(util::fmt_sprintf(
                                "task [%s] parse faild, err invalid cron: %s",
                                task.name.c_str(), task.cron.c_str()));
                        }
                    }
                } else {
                    continue;
                }

                if (es.exec_window == 0 || es.exec_window > tn) continue;
                if (es.done_updated >= es.exec_window) continue;
                if (!depend_allow(task)) continue;

                if (params.empty()) {
                    params = template_::var_params(app);
                }

                if (task_async_action(task, es, params) != 0) {
                    es.done_updated = 0;
                    es.fail_updated =
                        std::max(util::now_unix(), es.exec_window);
                    log::info(util::fmt_sprintf(
                        "task [%s] stats, msg exec failed", task.name.c_str()));
                }

                std::this_thread::sleep_for(std::chrono::milliseconds(10));
            }

            return 0;
        }

        void on_startup_aggregate(const model::AppReplicaInstance& app,
                                  std::string& state, std::string& msg) {
            state.clear();
            msg.clear();

            int pending = 0, running = 0, failed = 0, total = 0;
            std::string failed_name;
            for (const auto& t : app.app.spec.tasks) {
                if (!t.on_startup) continue;
                total++;
                auto it = g_exec_statuses.find(t.name);
                if (it == g_exec_statuses.end()) {
                    pending++;
                    continue;
                }
                const ExecutorStatus& es = it->second;
                if (es.fail_updated > 0 && es.done_updated < es.fail_updated) {
                    failed++;
                    if (failed_name.empty()) failed_name = t.name;
                } else if (es.done_updated > 0) {
                    // succeeded
                } else if (es.state == "running") {
                    running++;
                } else {
                    pending++;
                }
            }

            if (total == 0) {
                state = "success";
                return;
            }
            if (failed > 0) {
                state = "failed";
                msg = util::fmt_sprintf("%d/%d tasks failed (first: %s)",
                                        failed, total, failed_name.c_str());
            } else if (running > 0 || pending > 0) {
                state = "running";
                msg = util::fmt_sprintf("%d/%d tasks running", total - pending,
                                        total);
            } else {
                state = "success";
                msg = util::fmt_sprintf("%d/%d tasks done", total, total);
            }
        }

        int kill_all() {
            if (g_app.app.spec.tasks.empty()) return 0;

            auto params = template_::var_params(g_app);

            // kill running processes
            for (const auto& task : g_app.app.spec.tasks) {
                auto it = g_exec_statuses.find(task.name);
                if (it != g_exec_statuses.end() && it->second.child_pid > 0) {
                    pid_t pid = it->second.child_pid;
                    ::kill(pid, SIGKILL);
                    // bounded reap; do not hang PID 1 on a D-state child.
                    int status;
                    int64_t reap_deadline = util::now_unix_ms() + 3000;
                    while (util::now_unix_ms() < reap_deadline) {
                        if (waitpid(pid, &status, WNOHANG) > 0) break;
                        std::this_thread::sleep_for(
                            std::chrono::milliseconds(10));
                    }
                    it->second.child_pid = 0;
                    it->second.stdout_fd = -1;
                    // let the reader thread finish draining and release its fd
                    it->second.reader.reset();
                    log::info(
                        util::fmt_sprintf("task [%s] kill", task.name.c_str()));
                }
            }

            // run shutdown tasks synchronously
            for (const auto& task : g_app.app.spec.tasks) {
                if (!task.on_shutdown) continue;
                if (task_sync_action(task, params) != 0) {
                    log::info(util::fmt_sprintf("task [%s] kill failed",
                                                task.name.c_str()));
                } else {
                    log::info(util::fmt_sprintf("task [%s] kill ok",
                                                task.name.c_str()));
                }
            }

            return 0;
        }

        void reap_children() {
            // Reap tracked task children
            for (auto& kv : g_exec_statuses) {
                ExecutorStatus& es = kv.second;
                if (es.child_pid > 0) {
                    int status;
                    pid_t result = waitpid(es.child_pid, &status, WNOHANG);
                    if (result > 0) {
                        apply_exit(es, status, kv.first);
                    }
                }
            }

            // Reap any untracked orphaned children (PID 1 responsibility).
            // When inagent runs as PID 1, orphaned processes are re-parented to
            // it and must be explicitly reaped to prevent zombie accumulation.
            int status;
            while (waitpid(-1, &status, WNOHANG) > 0) {
            }
        }

    } // namespace task
} // namespace inagent
