# innerstack

[简体中文](./README.zh-CN.md)

**A lightweight container scheduling and management engine for private PaaS.**
It delivers industrial-grade stability through a radically simplified architecture.

- **Zero external dependencies** — Built-in data store, networking, DNS, and gateway.
- **Single binary, all roles** — Zone leader, host agent, and scheduler in one binary.
- **Extremely lightweight** — Minimal resource footprint with no unnecessary complexity.
- **Production-ready reliability** — Self-healing lifecycle, authenticated API, automatic leader election.

## Features

- **Container Orchestration** — Intelligent scheduling, multi-replica deployment, self-healing lifecycle management with automatic recovery, and fine-grained resource isolation.
- **Built-in VPC Networking** — Zero-config VXLAN overlay network with automatic IP address management and route propagation. Full zone-wide container connectivity out of the box.
- **Service Discovery** — Built-in DNS with automatic name resolution for all application instances, updated in real-time as containers are scheduled and terminated.
- **HTTP Gateway** — Domain-based routing with automatic TLS (Let's Encrypt), gzip compression, IP rate limiting, and load balancing.
- **Package Distribution** — Integrated package repository with resumable chunked upload/download and integrity verification.
- **Application Lifecycle** — Task engine supporting startup, shutdown, periodic, and cron-scheduled tasks with dependency ordering and automatic retries. Template rendering for dynamic configuration injection.
- **Security** — Cryptographically signed access keys with scoped permissions. Automatic deterministic leader election with no external coordination.

## Project Status

innerstack v2 is currently in **active development** (alpha stage). The architecture and core functionality are stable; features are being added incrementally.

## License

Licensed under the [Apache License 2.0](./LICENSE).
