// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
// Licensed under the Apache License, Version 2.0.

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <map>
#include <nlohmann/json.hpp>
#include <string>
#include <vector>

#include "config/app_config.h"
#include "config_merge/merger.h"
#include "daemon/daemon.h"
#include "template/render.h"
#include "template/var_params.h"
#include "util/string_util.h"

using namespace inagent;

static const char* kVersion = "2.0.0-dev";

static void print_usage() {
    printf("inagent %s - An Efficient Enterprise PaaS Engine\n\n", kVersion);
    printf("Usage:\n");
    printf("  inagent [flags] <command>\n\n");
    printf("Available Commands:\n");
    printf("  daemon          run inagent in daemon mode\n");
    printf(
        "  config-render   read input file and render with config data, then "
        "write to output file\n");
    printf(
        "  config-merge    merge one of input text (json, yaml, toml, ini) to "
        "local config file\n");
    printf("  config-export   export config data\n\n");
    printf("Flags:\n");
    printf(
        "  --prefix string   specify the home directory of project (default "
        "\"/home/action\")\n");
    printf("  --version         print version\n");
    printf("  --help            print help\n");
}

static int cmd_config_export(const std::string& prefix) {
    config::AppConfigHelper helper(prefix);
    if (!helper.load()) {
        fprintf(stderr, "failed to load app config\n");
        return 1;
    }

    auto params = helper.params();
    nlohmann::json j = params;
    printf("\n%s\n\n", j.dump(2).c_str());
    return 0;
}

static int cmd_config_render(const std::string& prefix,
                             const std::string& input_file,
                             const std::string& output_file) {
    if (input_file.empty()) {
        fprintf(stderr, "Error: --in input file not setup\n");
        return 1;
    }
    if (output_file.empty()) {
        fprintf(stderr, "Error: --out output file not setup\n");
        return 1;
    }

    config::AppConfigHelper helper(prefix);
    if (!helper.load()) {
        fprintf(stderr, "failed to load app config\n");
        return 1;
    }

    if (template_::file_render(output_file, input_file, helper.params(),
                               0640) != 0) {
        fprintf(stderr, "failed to render file\n");
        return 1;
    }
    return 0;
}

static int cmd_config_merge(const std::string& prefix,
                            const std::string& with_config_field,
                            const std::string& config_file) {
    std::string field_name = with_config_field;
    // strip "cfg/" prefix if present
    if (field_name.size() > 4 && field_name.substr(0, 4) == "cfg/") {
        field_name = field_name.substr(4);
    }
    if (field_name.empty()) {
        fprintf(stderr, "Error: invalid --with-config-field value\n");
        return 1;
    }
    if (config_file.empty()) {
        fprintf(stderr, "Error: --config file path not found\n");
        return 1;
    }

    config::AppConfigHelper helper(prefix);
    if (!helper.load()) {
        fprintf(stderr, "failed to load app config\n");
        return 1;
    }

    auto* field = helper.config(field_name);
    if (!field) {
        fprintf(stderr, "Error: config field (%s) not found\n",
                field_name.c_str());
        return 1;
    }

    std::string value = util::trim(field->value);
    if (value.empty()) return 0;

    auto params = helper.params();
    value = template_::render_with_expand(value, params);

    config_merge::ConfigType type = config_merge::ConfigType::UNKNOWN;
    if (!field->type.empty()) {
        if (field->type == "text_json")
            type = config_merge::ConfigType::JSON;
        else if (field->type == "text_toml")
            type = config_merge::ConfigType::TOML;
        else if (field->type == "text_yaml")
            type = config_merge::ConfigType::YAML;
        else if (field->type == "text_ini")
            type = config_merge::ConfigType::INI;
        else if (field->type == "text_javaprop")
            type = config_merge::ConfigType::JAVA_PROP;
        else {
            fprintf(stderr, "Error: field type(%s) not support\n",
                    field->type.c_str());
            return 1;
        }
    } else {
        type = config_merge::infer_type_from_ext(config_file);
        if (type == config_merge::ConfigType::UNKNOWN) {
            fprintf(stderr,
                    "Error: cannot infer config type from file extension\n");
            return 1;
        }
    }

    if (config_merge::config_merge(config_file, value, type) != 0) {
        fprintf(stderr, "Error: config merge failed\n");
        return 1;
    }
    return 0;
}

int main(int argc, char* argv[]) {
    std::string prefix = "/home/action";
    std::string command;

    // parse global flags and command
    int i = 1;
    for (; i < argc; ++i) {
        std::string arg = argv[i];
        if (arg == "--help" || arg == "-h") {
            print_usage();
            return 0;
        } else if (arg == "--version" || arg == "-v") {
            printf("inagent %s\n", kVersion);
            return 0;
        } else if (arg == "--prefix" && i + 1 < argc) {
            prefix = argv[++i];
        } else if (arg.substr(0, 9) == "--prefix=") {
            prefix = arg.substr(9);
        } else {
            command = arg;
            break;
        }
    }

    if (command.empty()) {
        print_usage();
        return 1;
    }

    // collect remaining args for subcommand
    std::vector<std::string> cmd_args;
    for (++i; i < argc; ++i) {
        cmd_args.emplace_back(argv[i]);
    }

    if (command == "daemon") {
        return agent_daemon::run(prefix);
    }

    if (command == "config-export") {
        return cmd_config_export(prefix);
    }

    if (command == "config-render" || command == "confrender") {
        std::string input_file, output_file;
        for (size_t j = 0; j < cmd_args.size(); ++j) {
            if ((cmd_args[j] == "--in" || cmd_args[j] == "-i") &&
                j + 1 < cmd_args.size()) {
                input_file = cmd_args[++j];
            } else if (cmd_args[j].substr(0, 5) == "--in=") {
                input_file = cmd_args[j].substr(5);
            } else if ((cmd_args[j] == "--out" || cmd_args[j] == "-o") &&
                       j + 1 < cmd_args.size()) {
                output_file = cmd_args[++j];
            } else if (cmd_args[j].substr(0, 6) == "--out=") {
                output_file = cmd_args[j].substr(6);
            }
        }
        return cmd_config_render(prefix, input_file, output_file);
    }

    if (command == "config-merge") {
        std::string with_config_field, config_file;
        for (size_t j = 0; j < cmd_args.size(); ++j) {
            if (cmd_args[j] == "--with-config-field" &&
                j + 1 < cmd_args.size()) {
                with_config_field = cmd_args[++j];
            } else if (cmd_args[j].substr(0, 20) == "--with-config-field=") {
                with_config_field = cmd_args[j].substr(20);
            } else if (cmd_args[j] == "--config" && j + 1 < cmd_args.size()) {
                config_file = cmd_args[++j];
            } else if (cmd_args[j].substr(0, 9) == "--config=") {
                config_file = cmd_args[j].substr(9);
            }
        }
        return cmd_config_merge(prefix, with_config_field, config_file);
    }

    fprintf(stderr, "Error: unknown command '%s'\n", command.c_str());
    print_usage();
    return 1;
}
