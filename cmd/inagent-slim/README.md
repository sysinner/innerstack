# inagent-slim

C++ implementation of inagent (slim build), the container-internal agent process for InnerStack PaaS.
Goal: significantly smaller binary and lower runtime memory than the Go version.

| | Go | C++ |
|---|---|---|
| Binary size (stripped) | ~8 MB | ~800 KB |
| Runtime deps | glibc/musl | none (static) |
| Standard | Go 1.x | C++17 |

## Features

Full parity with the Go version:

- **daemon** -- long-running task executor inside containers
  - Periodic polling of `app_replica.json`
  - Task scheduling with `on_startup` / `on_shutdown` / `interval_seconds` / `cron` triggers
  - Dependency ordering between tasks
  - Retry with minimum 10-second gap
  - Script execution via `sh` with `set -e; set -o pipefail`
  - User switching: `root` (uid=0) or `action` (uid=2048)
  - Graceful shutdown on SIGTERM/SIGINT

- **config-render** -- template file rendering with `${VAR}` variable substitution

- **config-merge** -- merge config values into local files (JSON, TOML, YAML, INI, Java Properties)

- **config-export** -- dump all resolved template variables as JSON to stdout

## Build

### Docker (recommended, produces static Linux binary)

```bash
make inagent-slim
```

Output: `bin/inagent-slim-linux-amd64`, `bin/inagent-slim-linux-arm64`

### Local development (macOS/Linux)

```bash
cd cmd/inagent-slim
mkdir build && cd build
cmake -DCMAKE_BUILD_TYPE=Debug ..
make -j$(nproc)
```

### Release build with static linking (Linux only)

```bash
cmake -DCMAKE_BUILD_TYPE=Release -DSTATIC_LINK=ON ..
make -j$(nproc)
```

For cross-compilation, use toolchain files:

```bash
cmake -DCMAKE_TOOLCHAIN_FILE=../toolchain/linux-amd64.cmake -DCMAKE_BUILD_TYPE=Release -DSTATIC_LINK=ON ..
cmake -DCMAKE_TOOLCHAIN_FILE=../toolchain/linux-arm64.cmake -DCMAKE_BUILD_TYPE=Release -DSTATIC_LINK=ON ..
```

## Usage

```
inagent [flags] <command>

Commands:
  daemon            run as long-lived agent process
  config-render     render template file with variable substitution
  config-merge      merge config field into local config file
  config-export     export all resolved variables as JSON

Flags:
  --prefix string   home directory (default "/home/action")
  --version         print version
```

### config-render

```bash
inagent config-render --in template.conf --out /etc/app/app.conf
```

### config-merge

```bash
inagent config-merge --with-config-field server_ini --config /etc/app/server.ini
```

### config-export

```bash
inagent config-export
```

### daemon

```bash
export APP_HOST_ID=a1b2c3d4e5f6
export APP_NAME=myapp
export APP_REP_ID=0
inagent daemon
```

Required environment variables:

| Variable | Format | Example |
|---|---|---|
| `APP_HOST_ID` | 12-16 hex chars | `a1b2c3d4e5f6` |
| `APP_NAME` | RFC 1123 DNS label (3-63) | `myapp` |
| `APP_REP_ID` | 0-255 | `0` |

## Template Variables

The template engine substitutes `${VAR_NAME}` patterns. Unresolved variables are left unchanged.

| Key Pattern | Example |
|---|---|
| `app.name` | `0123456789ab` |
| `app.replica.rep_id` | `0` |
| `app.deploy.replica_cap` | `3` |
| `self.cfg.{name}` | current app config value |
| `self.net.{port}.internal_addr/host/port` | VPC or local address |
| `self.net.{port}.service_addr/host/port` | DNS service endpoint |
| `deps.{dep}.cfg.{name}` | dependency config value |
| `deps.{dep}.net.{port}.internal_addr/host/port` | dependency address |
| `deps.{dep}.net.{port}.service_addr/host/port` | dependency DNS endpoint |
| `ipk.{pkg}.path` | `/usr/innerstack/{pkg}` |

Address resolution priority: VPC IPv4 -> Host IPv4 + Host Port -> 127.0.0.1

## Directory Structure

```
cmd/inagent-slim/
  CMakeLists.txt
  Dockerfile
  src/
    main.cpp                    entry point, CLI
    model/types.h/.cpp          data structures, JSON bindings
    config/app_config.h/.cpp    config file loader
    template/var_params.h/.cpp  variable map generation
    template/render.h/.cpp      template substitution
    config_merge/merger.h/.cpp  multi-format config merge
    daemon/daemon.h/.cpp        daemon event loop
    task/engine.h/.cpp          task scheduling engine
    task/cron.h/.cpp            cron expression parser
    task/executor.h             process execution structs
    log/logger.h/.cpp           JSON structured logging
    signal/signal_handler.h/.cpp signal handling
    util/                       filesystem, string, time utilities
  third_party/
    nlohmann/json               JSON (header-only, MIT)
    toml11                      TOML (header-only, MIT)
    yaml-cpp                    YAML (static lib, MIT)
  toolchain/
    linux-amd64.cmake
    linux-arm64.cmake
```

## Third-Party Libraries

| Library | Version | Purpose | License |
|---|---|---|---|
| nlohmann/json | 3.11.3 | JSON parse/serialize | MIT |
| toml11 | 4.2.0 | TOML parse/serialize | MIT |
| yaml-cpp | 0.9.0 | YAML parse/serialize | MIT |

No protobuf dependency. Data structures use nlohmann/json `from_json` bindings matching the JSON field names from `app_replica.json`.
