// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include "model/types.h"

namespace inagent {
    namespace model {

        // All fields are accessed via contains() guards so that a missing field
        // in app_replica.json leaves the default value rather than throwing.
        // This mirrors the Go side, where the protobuf-generated structs
        // unmarshal missing JSON fields as zero values without error.

        void from_json(const nlohmann::json& j, Metadata& v) {
            if (j.contains("id")) j.at("id").get_to(v.id);
            if (j.contains("name")) j.at("name").get_to(v.name);
            if (j.contains("user")) j.at("user").get_to(v.user);
            if (j.contains("created")) j.at("created").get_to(v.created);
            if (j.contains("updated")) j.at("updated").get_to(v.updated);
            if (j.contains("version")) j.at("version").get_to(v.version);
        }

        void from_json(const nlohmann::json& j, AppSpecResources& v) {
            if (j.contains("cpu_limit")) j.at("cpu_limit").get_to(v.cpu_limit);
            if (j.contains("memory_limit"))
                j.at("memory_limit").get_to(v.memory_limit);
            if (j.contains("volume_limit"))
                j.at("volume_limit").get_to(v.volume_limit);
        }

        void from_json(const nlohmann::json& j, AppSpecPackage& v) {
            if (j.contains("name")) j.at("name").get_to(v.name);
            if (j.contains("version")) j.at("version").get_to(v.version);
        }

        void from_json(const nlohmann::json& j, AppSpecServicePort& v) {
            if (j.contains("name")) j.at("name").get_to(v.name);
            if (j.contains("port")) j.at("port").get_to(v.port);
        }

        void from_json(const nlohmann::json& j, AppSpecTask& v) {
            if (j.contains("name")) j.at("name").get_to(v.name);
            if (j.contains("working_dir"))
                j.at("working_dir").get_to(v.working_dir);
            if (j.contains("run_user")) j.at("run_user").get_to(v.run_user);
            if (j.contains("depends_on"))
                j.at("depends_on").get_to(v.depends_on);
            if (j.contains("on_startup"))
                j.at("on_startup").get_to(v.on_startup);
            if (j.contains("on_shutdown"))
                j.at("on_shutdown").get_to(v.on_shutdown);
            if (j.contains("interval_seconds"))
                j.at("interval_seconds").get_to(v.interval_seconds);
            if (j.contains("cron")) j.at("cron").get_to(v.cron);
            if (j.contains("max_attempts"))
                j.at("max_attempts").get_to(v.max_attempts);
            if (j.contains("retry_backoff_seconds"))
                j.at("retry_backoff_seconds").get_to(v.retry_backoff_seconds);
            if (j.contains("script")) j.at("script").get_to(v.script);
        }

        void from_json(const nlohmann::json& j, AppSpecConfigItem& v) {
            if (j.contains("name")) j.at("name").get_to(v.name);
            if (j.contains("title")) j.at("title").get_to(v.title);
            if (j.contains("prompt")) j.at("prompt").get_to(v.prompt);
            if (j.contains("type")) j.at("type").get_to(v.type);
            if (j.contains("default")) j.at("default").get_to(v.default_value);
            if (j.contains("description"))
                j.at("description").get_to(v.description);
            if (j.contains("items")) j.at("items").get_to(v.items);
        }

        void from_json(const nlohmann::json& j, AppSpecDepend& v) {
            if (j.contains("name")) j.at("name").get_to(v.name);
            if (j.contains("version")) j.at("version").get_to(v.version);
        }

        void from_json(const nlohmann::json& j, AppSpec& v) {
            if (j.contains("kind")) j.at("kind").get_to(v.kind);
            if (j.contains("name")) j.at("name").get_to(v.name);
            if (j.contains("version")) j.at("version").get_to(v.version);
            if (j.contains("description"))
                j.at("description").get_to(v.description);
            if (j.contains("image")) j.at("image").get_to(v.image);
            if (j.contains("resources")) j.at("resources").get_to(v.resources);
            if (j.contains("depends")) j.at("depends").get_to(v.depends);
            if (j.contains("service_depends"))
                j.at("service_depends").get_to(v.service_depends);
            if (j.contains("service_ports"))
                j.at("service_ports").get_to(v.service_ports);
            if (j.contains("packages")) j.at("packages").get_to(v.packages);
            if (j.contains("configs")) j.at("configs").get_to(v.configs);
            if (j.contains("tasks")) j.at("tasks").get_to(v.tasks);
        }

        void from_json(const nlohmann::json& j, AppDeployConfigItem& v) {
            if (j.contains("name")) j.at("name").get_to(v.name);
            if (j.contains("value")) j.at("value").get_to(v.value);
            if (j.contains("type")) j.at("type").get_to(v.type);
            if (j.contains("items")) j.at("items").get_to(v.items);
        }

        void from_json(const nlohmann::json& j, AppDeployServicePort& v) {
            if (j.contains("name")) j.at("name").get_to(v.name);
            if (j.contains("port")) j.at("port").get_to(v.port);
            if (j.contains("host_port")) j.at("host_port").get_to(v.host_port);
            if (j.contains("lan_addr")) j.at("lan_addr").get_to(v.lan_addr);
        }

        void from_json(const nlohmann::json& j, AppDeployReplica& v) {
            if (j.contains("id")) j.at("id").get_to(v.id);
            if (j.contains("host_id")) j.at("host_id").get_to(v.host_id);
            if (j.contains("host_ipv4")) j.at("host_ipv4").get_to(v.host_ipv4);
            if (j.contains("state")) j.at("state").get_to(v.state);
            if (j.contains("vpc_ipv4")) j.at("vpc_ipv4").get_to(v.vpc_ipv4);
            if (j.contains("service_ports"))
                j.at("service_ports").get_to(v.service_ports);
        }

        void from_json(const nlohmann::json& j, AppDeployDepend& v) {
            if (j.contains("spec_name")) j.at("spec_name").get_to(v.spec_name);
            if (j.contains("instance_name"))
                j.at("instance_name").get_to(v.instance_name);
            if (j.contains("configs")) j.at("configs").get_to(v.configs);
            if (j.contains("replicas")) j.at("replicas").get_to(v.replicas);
        }

        void from_json(const nlohmann::json& j, AppDeploy& v) {
            if (j.contains("action")) j.at("action").get_to(v.action);
            if (j.contains("revision")) j.at("revision").get_to(v.revision);
            if (j.contains("cpu_limit")) j.at("cpu_limit").get_to(v.cpu_limit);
            if (j.contains("memory_limit"))
                j.at("memory_limit").get_to(v.memory_limit);
            if (j.contains("volume_limit"))
                j.at("volume_limit").get_to(v.volume_limit);
            if (j.contains("configs")) j.at("configs").get_to(v.configs);
            if (j.contains("replica_cap"))
                j.at("replica_cap").get_to(v.replica_cap);
            if (j.contains("replicas")) j.at("replicas").get_to(v.replicas);
            if (j.contains("depends")) j.at("depends").get_to(v.depends);
        }

        void from_json(const nlohmann::json& j, AppInstance& v) {
            if (j.contains("meta")) j.at("meta").get_to(v.meta);
            if (j.contains("spec")) j.at("spec").get_to(v.spec);
            if (j.contains("deploy")) j.at("deploy").get_to(v.deploy);
            if (j.contains("revision")) j.at("revision").get_to(v.revision);
            if (j.contains("ref_by_instances"))
                j.at("ref_by_instances").get_to(v.ref_by_instances);
        }

        void from_json(const nlohmann::json& j, AppReplicaInstance& v) {
            if (j.contains("app")) j.at("app").get_to(v.app);
            if (j.contains("replica")) j.at("replica").get_to(v.replica);
            if (j.contains("zone_base_domain"))
                j.at("zone_base_domain").get_to(v.zone_base_domain);
            if (j.contains("hostlet_endpoint"))
                j.at("hostlet_endpoint").get_to(v.hostlet_endpoint);
        }

        void from_json(const nlohmann::json& j, HostletStatusEndpoint& v) {
            if (j.contains("url")) j.at("url").get_to(v.url);
            if (j.contains("secret_key"))
                j.at("secret_key").get_to(v.secret_key);
        }

    } // namespace model
} // namespace inagent
