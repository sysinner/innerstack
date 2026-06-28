// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include "status/reporter.h"

#include <netdb.h>
#include <netinet/in.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <unistd.h>

#include <cerrno>
#include <cstring>
#include <nlohmann/json.hpp>
#include <sstream>
#include <string>
#include <vector>

#include "log/logger.h"
#include "util/string_util.h"
#include "util/time_util.h"

namespace inagent {
    namespace status {

        const char* const kStatePending = "pending";
        const char* const kStateRunning = "running";
        const char* const kStateSuccess = "success";
        const char* const kStateFailed = "failed";

        const char* const kStageInagentBoot = "inagent_boot";
        const char* const kStageSpecLoad = "spec_load";
        const char* const kStageTaskRun = "task_run";

        static const char* const kOwner = "inagent";
        static const int64_t kHeartbeatMs = 30000;
        static const int kHttpTimeoutSec = 3;

        struct Stage {
            std::string name;
            std::string owner = kOwner;
            uint64_t revision = 0;
            std::string state = kStatePending;
            int64_t created = 0;
            int64_t finished = 0;
            std::string message;
            int64_t attempt = 0;
        };

        static std::vector<Stage> g_stages;
        static bool g_dirty = false;
        static uint64_t g_revision = 0;
        static int64_t g_last_flush_ms = 0;

        static Stage* find_stage(const std::string& name) {
            for (auto& s : g_stages) {
                if (s.name == name) return &s;
            }
            return nullptr;
        }

        static Stage* child(const std::string& name) {
            Stage* s = find_stage(name);
            if (s) return s;
            Stage ns;
            ns.name = name;
            ns.owner = kOwner;
            ns.state = kStatePending;
            g_stages.push_back(std::move(ns));
            return &g_stages.back();
        }

        static void set_running(Stage& s, const std::string& msg) {
            if (s.state == kStateFailed) s.attempt++;
            if (s.created == 0) s.created = util::now_unix_ms();
            s.finished = 0;
            s.state = kStateRunning;
            if (!msg.empty()) s.message = msg;
        }

        static void set_success(Stage& s, const std::string& msg) {
            if (s.created == 0) s.created = util::now_unix_ms();
            s.finished = util::now_unix_ms();
            s.state = kStateSuccess;
            if (!msg.empty()) s.message = msg;
        }

        static void set_failed(Stage& s, const std::string& msg) {
            if (s.created == 0) s.created = util::now_unix_ms();
            s.finished = util::now_unix_ms();
            s.state = kStateFailed;
            if (!msg.empty()) s.message = msg;
        }

        static void set_instant(Stage& s, const std::string& msg) {
            int64_t now = util::now_unix_ms();
            s.created = now;
            s.finished = now;
            s.state = kStateSuccess;
            if (!msg.empty()) s.message = msg;
        }

        static void apply(const std::string& name, const std::string& state,
                          const std::string& msg) {
            Stage& s = *child(name);
            if (s.state == state && s.message == msg &&
                s.revision == g_revision) {
                return;
            }
            s.owner = kOwner;
            s.revision = g_revision;
            if (state == kStateRunning) {
                set_running(s, msg);
            } else if (state == kStateSuccess) {
                set_success(s, msg);
            } else if (state == kStateFailed) {
                set_failed(s, msg);
            }
            g_dirty = true;
        }

        void set_revision(uint64_t rev) {
            if (g_revision == rev) return;
            g_revision = rev;
            g_stages.clear();
            g_dirty = true;
        }

        void set_boot() {
            Stage& s = *child(kStageInagentBoot);
            if (s.state == kStateSuccess && s.revision == g_revision) return;
            s.owner = kOwner;
            s.revision = g_revision;
            set_instant(s, "");
            g_dirty = true;
        }

        void set_spec_load(const std::string& state, const std::string& msg) {
            apply(kStageSpecLoad, state, msg);
        }

        void set_task_run(const std::string& state, const std::string& msg) {
            apply(kStageTaskRun, state, msg);
        }

        // Parse "http://host:port/path" into host, port, path. Returns false
        // on malformed input.
        static bool parse_url(const std::string& url, std::string& host,
                              int& port, std::string& path) {
            const std::string scheme = "http://";
            if (url.compare(0, scheme.size(), scheme) != 0) return false;
            std::string rest = url.substr(scheme.size());
            size_t slash = rest.find('/');
            std::string hostport =
                (slash == std::string::npos) ? rest : rest.substr(0, slash);
            path = (slash == std::string::npos) ? "/" : rest.substr(slash);
            size_t colon = hostport.rfind(':');
            if (colon == std::string::npos) return false;
            host = hostport.substr(0, colon);
            port = std::atoi(hostport.substr(colon + 1).c_str());
            if (host.empty() || port <= 0) return false;
            return true;
        }

        // POST body to url with the secret header. Returns the HTTP status
        // code, or -1 on connection/IO failure.
        static int http_post(const std::string& url, const std::string& secret,
                             const std::string& body) {
            std::string host;
            int port = 0;
            std::string path;
            if (!parse_url(url, host, port, path)) {
                log::warn("inagent status post: invalid url", "url", url);
                return -1;
            }

            struct addrinfo hints{};
            hints.ai_family = AF_INET;
            hints.ai_socktype = SOCK_STREAM;
            struct addrinfo* res = nullptr;
            if (getaddrinfo(host.c_str(), std::to_string(port).c_str(), &hints,
                            &res) != 0 ||
                !res) {
                log::warn("inagent status post: resolve failed", "url", url);
                return -1;
            }

            int fd = -1;
            for (struct addrinfo* rp = res; rp; rp = rp->ai_next) {
                fd = socket(rp->ai_family, rp->ai_socktype, rp->ai_protocol);
                if (fd < 0) continue;
                struct timeval tv{};
                tv.tv_sec = kHttpTimeoutSec;
                setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, sizeof(tv));
                setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));
                if (connect(fd, rp->ai_addr, rp->ai_addrlen) == 0) break;
                close(fd);
                fd = -1;
            }
            freeaddrinfo(res);
            if (fd < 0) {
                log::warn("inagent status post: connect failed", "url", url);
                return -1;
            }

            std::ostringstream req;
            req << "POST " << path << " HTTP/1.0\r\n"
                << "Host: " << host << ":" << port << "\r\n"
                << "Content-Type: application/json\r\n"
                << "X-Secret-Key: " << secret << "\r\n"
                << "Content-Length: " << body.size() << "\r\n"
                << "Connection: close\r\n\r\n"
                << body;
            std::string req_str = req.str();

            const char* p = req_str.data();
            size_t left = req_str.size();
            while (left > 0) {
                ssize_t n = send(fd, p, left, 0);
                if (n <= 0) {
                    if (errno == EINTR) continue;
                    close(fd);
                    log::warn("inagent status post: send failed", "url", url);
                    return -1;
                }
                p += n;
                left -= static_cast<size_t>(n);
            }

            // Read the status line only.
            std::string line;
            char ch;
            while (recv(fd, &ch, 1, 0) == 1) {
                if (ch == '\n') break;
                if (ch != '\r') line += ch;
            }
            close(fd);

            // Parse "HTTP/1.0 200 OK".
            int code = -1;
            size_t sp = line.find(' ');
            if (sp != std::string::npos) {
                code = std::atoi(line.substr(sp + 1).c_str());
            }
            return code;
        }

        void flush(const model::HostletStatusEndpoint& endpoint,
                   const std::string& instance_name, uint32_t rep_id) {
            if (endpoint.url.empty()) return;

            int64_t now = util::now_unix_ms();
            if (!g_dirty && (now - g_last_flush_ms) < kHeartbeatMs) return;

            nlohmann::json j_stages = nlohmann::json::array();
            for (const auto& s : g_stages) {
                nlohmann::json j;
                j["name"] = s.name;
                j["owner"] = s.owner;
                j["revision"] = s.revision;
                j["state"] = s.state;
                j["attempt"] = s.attempt;
                j["created"] = s.created;
                j["finished"] = s.finished;
                if (!s.message.empty()) j["message"] = s.message;
                j_stages.push_back(std::move(j));
            }

            nlohmann::json body;
            body["instance_name"] = instance_name;
            body["replica_id"] = rep_id;
            body["updated"] = now;
            body["stages"] = std::move(j_stages);

            std::string body_str = body.dump();
            size_t stage_count = g_stages.size();

            int code = http_post(endpoint.url, endpoint.secret_key, body_str);
            if (code == 200) {
                g_dirty = false;
                g_last_flush_ms = util::now_unix_ms();
                log::info("inagent status posted", "stages",
                          std::to_string(stage_count));
            } else if (code > 0) {
                log::warn("inagent status post rejected", "status",
                          std::to_string(code));
            }
            // code < 0: connection failure already logged in http_post; retry
            // next tick.
        }

    } // namespace status
} // namespace inagent
