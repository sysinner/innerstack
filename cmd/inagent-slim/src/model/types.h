// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#pragma once

#include <cstdint>
#include <nlohmann/json.hpp>
#include <string>
#include <vector>

namespace inagent {
    namespace model {

        struct Metadata {
            std::string id;
            std::string name;
            std::string user;
            int64_t created = 0;
            int64_t updated = 0;
            uint64_t version = 0;
        };

        struct AppSpecResources {
            std::string cpu_limit;
            std::string memory_limit;
            std::string volume_limit;
        };

        struct AppSpecPackage {
            std::string name;
            std::string version;
        };

        struct AppSpecServicePort {
            std::string name;
            uint32_t port = 0;
        };

        struct AppSpecTask {
            std::string name;
            std::string working_dir;
            std::string run_user;
            std::vector<std::string> depends_on;
            bool on_startup = false;
            bool on_shutdown = false;
            int64_t interval_seconds = 0;
            std::string cron;
            uint32_t max_attempts = 0;
            uint32_t retry_backoff_seconds = 0;
            std::string script;
        };

        struct AppSpecConfigItem {
            std::string name;
            std::string title;
            std::string prompt;
            std::string type;
            std::string default_value;
            std::string description;
            std::vector<AppSpecConfigItem> items;
        };

        struct AppSpecDepend {
            std::string name;
            std::string version;
        };

        struct AppSpec {
            std::string kind;
            std::string name;
            std::string version;
            std::string description;
            std::string image;
            AppSpecResources resources;
            std::vector<AppSpecDepend> depends;
            std::vector<AppSpecDepend> service_depends;
            std::vector<AppSpecServicePort> service_ports;
            std::vector<AppSpecPackage> packages;
            std::vector<AppSpecConfigItem> configs;
            std::vector<AppSpecTask> tasks;
        };

        struct AppDeployConfigItem {
            std::string name;
            std::string value;
            std::string type;
            std::vector<AppDeployConfigItem> items;
        };

        struct AppDeployServicePort {
            std::string name;
            uint32_t port = 0;
            uint32_t host_port = 0;
            std::string lan_addr;
        };

        struct AppDeployReplica {
            uint32_t id = 0;
            std::string host_id;
            std::string host_ipv4;
            std::string state;
            std::string vpc_ipv4;
            std::vector<AppDeployServicePort> service_ports;
        };

        struct AppDeployDepend {
            std::string spec_name;
            std::string instance_name;
            std::vector<AppDeployConfigItem> configs;
            std::vector<AppDeployReplica> replicas;
        };

        struct AppDeploy {
            std::string action;
            uint64_t revision = 0;
            int64_t cpu_limit = 0;
            int64_t memory_limit = 0;
            int64_t volume_limit = 0;
            std::vector<AppDeployConfigItem> configs;
            uint32_t replica_cap = 0;
            std::vector<AppDeployReplica> replicas;
            std::vector<AppDeployDepend> depends;
        };

        struct AppInstance {
            Metadata meta;
            AppSpec spec;
            AppDeploy deploy;
            uint64_t revision = 0;
            std::vector<std::string> ref_by_instances;
        };

        struct AppReplicaInstance {
            AppInstance app;
            AppDeployReplica replica;
            std::string zone_base_domain;
        };

        // JSON deserialization
        void from_json(const nlohmann::json& j, Metadata& v);
        void from_json(const nlohmann::json& j, AppSpecResources& v);
        void from_json(const nlohmann::json& j, AppSpecPackage& v);
        void from_json(const nlohmann::json& j, AppSpecServicePort& v);
        void from_json(const nlohmann::json& j, AppSpecTask& v);
        void from_json(const nlohmann::json& j, AppSpecConfigItem& v);
        void from_json(const nlohmann::json& j, AppSpecDepend& v);
        void from_json(const nlohmann::json& j, AppSpec& v);
        void from_json(const nlohmann::json& j, AppDeployConfigItem& v);
        void from_json(const nlohmann::json& j, AppDeployServicePort& v);
        void from_json(const nlohmann::json& j, AppDeployReplica& v);
        void from_json(const nlohmann::json& j, AppDeployDepend& v);
        void from_json(const nlohmann::json& j, AppDeploy& v);
        void from_json(const nlohmann::json& j, AppInstance& v);
        void from_json(const nlohmann::json& j, AppReplicaInstance& v);

    } // namespace model
} // namespace inagent
