# InnerStack Packaging (nfpm)

Cross-platform packaging for InnerStack using [nfpm](https://nfpm.goreleaser.com/).
nfpm is a single static Go binary that runs natively on macOS and Linux and
produces `.rpm` / `.deb` archives from YAML configs — no `rpmbuild`, no root
required on the build host. Go binaries are cross-compiled via `GOARCH`, so a
macOS laptop can produce linux/amd64 + linux/arm64 packages directly. The
inagent-slim (C++) binary is built via Docker (`make inagent-slim-<arch>`);
pass `--without-inagent-slim` to skip it and avoid the Docker dependency.

## Components / Packages

| Package             | Binary        | Required |
|---------------------|---------------|----------|
| `innerstack`        | `innerstackd` + `inagent-linux-<arch>` (+ `inagent-slim-linux-<arch>`) | Yes |
| `innerstack-ingate` | `ingated`     | No       |
| `innerstack-indns`  | `indnsd`      | No       |
| `innerstack-cli`    | `innerstack`  | No       |

Subpackages `Depend: innerstack` (unversioned; always built and shipped
alongside the matching main package, so no exact-version pin is needed).

## Prerequisites

```bash
# Go toolchain >= 1.26 (for cross-compilation)
# nfpm:
#   macOS: brew install nfpm
#   Linux: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
# docker + make: required only for inagent-slim (default); skip with --without-inagent-slim
```

## Build

```bash
make pkg                          # current arch, rpm + deb, all components
make pkg-all-arch                 # amd64 + arm64
make rpm                          # rpm only
make deb                          # deb only
make pkg-clean                    # remove build artifacts

# or directly:
./misc/pkg/build.sh --all-arch
./misc/pkg/build.sh --arch arm64 --packager rpm --without-ingate --without-cli
./misc/pkg/build.sh --version 2.1.0
```

### Options

| Option              | Description                       | Default |
|---------------------|-----------------------------------|---------|
| `--arch <arch>`     | amd64 or arm64                    | host    |
| `--all-arch`        | Build for both amd64 and arm64    | no      |
| `--packager <fmt>`  | rpm or deb                        | both    |
| `--without-ingate`  | Exclude ingate subpackage         | include |
| `--without-indns`   | Exclude indns subpackage          | include |
| `--without-cli`     | Exclude CLI subpackage            | include |
| `--without-inagent-slim` | Exclude inagent-slim binary  | include |
| `--version <ver>`   | Package version                   | git tag |
| `--clean`           | Clean artifacts and exit          | no      |

Artifacts land in `build/rpm/` and `build/deb/` respectively
(e.g. `build/rpm/innerstack-2.0.0-1.x86_64.rpm`,
`build/deb/innerstack_2.0.0-1_amd64.deb`).

## Installation

```bash
# RPM
sudo rpm -ivh innerstack-<version>.<arch>.rpm        # required
sudo rpm -ivh innerstack-ingate-<version>.<arch>.rpm  # optional
sudo rpm -ivh innerstack-indns-<version>.<arch>.rpm   # optional
sudo rpm -ivh innerstack-cli-<version>.<arch>.rpm     # optional

# DEB
sudo apt install ./innerstack_<version>_<arch>.deb
sudo apt install ./innerstack-ingate_<version>_<arch>.deb

sudo systemctl enable --now innerstack
```

## Install Layout

```
/opt/innerstack/
  bin/
    innerstackd              # main daemon
    inagent-linux-<arch>     # container guest agent, Go build (target arch)
    inagent-slim-linux-<arch> # container guest agent, C++ build (target arch)
    ingated                  # HTTP gateway (optional)
    indnsd                   # DNS server (optional)
    innerstack               # CLI tool (optional)
  etc/
    innerstack.toml          # config (noreplace; auto-generated on first run)
    ingate.toml              # gateway config (optional)
    indnsd.toml              # DNS config (optional)
  var/log/                  # runtime logs
  cmd/inagent/ininit        # container init script
/etc or /usr/lib/systemd/system/
  innerstack.service
  ingate.service
  indns.service
```

## File Structure

```
misc/pkg/
  build.sh                   # cross-compile + stage + nfpm package
  nfpm.yaml                  # main package config
  nfpm.ingate.yaml           # ingate subpackage config
  nfpm.indns.yaml            # indns subpackage config
  nfpm.cli.yaml              # cli subpackage config
  scripts/
    <pkg>.postinstall        # %post / postinst scriptlets
    <pkg>.preremove          # %preun / prerm
    <pkg>.postremove         # %postun / postrm
```

systemd units are colocated with their binaries, not under `misc/pkg/`:
`cmd/server/innerstack.service`, `cmd/ingate/systemd.service`,
`cmd/indns/systemd.service`.

## Notes

- nfpm does not auto-generate systemd scriptlets; they are provided explicitly
  in `scripts/` and are written to handle both rpm (`$1` numeric) and deb
  (`configure` / `remove`) argument conventions.
- Config files use `type: config|noreplace` (RPM `%config(noreplace)`, deb
  conffile) so upgrades preserve local edits.
- `innerstack` ships the `inagent` (Go) and `inagent-slim` (C++) binaries for
  the target arch only — not both amd64 and arm64. The container guest agent
  runs inside images of the host's arch, so the matching arch suffices.

## Repository (per-generation DEB/RPM repos)

`build.sh --gen <id>` (required for every build — there is no flat/dev mode)
places packages directly into the repository layout, which **is** the HTTP
server storage path, and `misc/pkg/repo-sync.sh` creates the indexes (no
separate copy/staging step). Debian uses a shared pool across codenames via
[reprepro](https://wiki.debian.org/DebianRepository/SetupWithReprepro); RHEL
uses `createrepo` per arch in place. Both indexers run inside local builder
Docker images, so the host needs only `bash` + `docker` (reprepro/createrepo
are not required on the host). Output stays under `build/deb/` and `build/rpm/`
— rsync those two trees to the server; there is no upload logic in the scripts.

**Initial release supports RHEL 10 and Debian 13 (trixie) only.** The
generation machinery is data-driven via `misc/pkg/gen-map.txt`, so adding
distros later is a config change, not a code change.

### Generations

A **generation** groups codenames that share one ABI-compatible build (one
physical `.deb` in the pool). The **initial release targets Debian 13
(trixie) only**, so `gen-map.txt` lists just `trixie -> deb13`. The mechanism
scales unchanged: assign ABI-compatible codenames the same generation (a
future Debian 14/forky would reuse `deb13` and share trixie's `.deb`), and
open a new generation (e.g. `deb15`) only when a newer release breaks ABI
(soname change, libc/systemd break).

The single source of truth is `misc/pkg/gen-map.txt` (`codename -> generation`).
`build.sh --gen <id>` stamps `+<id>` into the package **release** (upstream
version is untouched) and places packages into the repo layout:
`build/deb/<id>/` for deb, `build/rpm/<id>/<arch>/` for rpm. Default (no
`--gen`) stays flat under `build/<fmt>/` for local dev.

RHEL generations are just directories `build/rpm/<gen>/<arch>/`, indexed in
place. A future ABI-compatible EL release reuses an existing generation via a
symlink — no rebuild, no separate index:

```bash
ln -s el10 build/rpm/el11     # el11 binary-compatible with el10 -> shares rpms + repodata
```

### Build + sync

```bash
# Build for the initial release — Debian 13 (trixie) + RHEL 10.
# `make deb`/`make rpm` already pass --gen + --all-arch:
make deb          # -> build/deb/deb13/*.deb
make rpm          # -> build/rpm/el10/<arch>/*.rpm
# (equivalently: ./misc/pkg/build.sh --packager deb --gen deb13 --all-arch, etc.)

# (once) build the local builder Docker images — or let `make repo` build them
# on demand when missing:
make repo-images

# Create indexes (Debian pool/dists under build/deb/repo/, RHEL repodata under build/rpm/):
make repo
```

`repo-sync.sh` records each generation's `.deb` (from `build/deb/<gen>/`) into
every codename of that generation; reprepro de-duplicates the pool, so a whole
generation maps to a single file. RHEL `createrepo` indexes each
`build/rpm/<gen>/<arch>/` in place.

### Builder images (Docker)

The distro-specific indexers run in two local builder images, defined under
`misc/pkg/docker/`:

| Image | Dockerfile | Provides |
|---|---|---|
| `innerstack-repo-deb:latest` | `Dockerfile.repo-deb` | `reprepro` (Debian) |
| `innerstack-repo-el:10` | `Dockerfile.repo-el` | `createrepo_c` (Rocky 10, mirrors RHEL 10) |

`repo-sync.sh` is dual-purpose: on the host it ensures the images exist (building
them if missing) and runs each side inside its container; invoked with
`--debian`/`--rhel` it builds just that side (used internally inside the
container). Build/refresh them explicitly with `make repo-images`
(`repo-sync.sh --build-images`).

### Generated tree

```
build/
├── deb/
│   ├── deb13/                  # incoming debs for gen deb13 (build.sh output)
│   └── repo/                   # reprepro repo (basedir): conf/ db/ dists/ pool/
│       ├── pool/main/...       # de-duplicated pool
│       ├── dists/trixie/...    # per-codename Packages/Release indexes
│       ├── conf/               # reprepro config
│       └── db/                 # reprepro state
└── rpm/
    └── el10/                   # RHEL repo: one dir per generation
        ├── x86_64/             # *.rpm + repodata/ (indexed in place)
        └── aarch64/            # *.rpm + repodata/
```

### Serve over HTTP

Static hosting is enough (nginx, caddy, `python3 -m http.server`). nginx:

```nginx
server {
    listen 443 ssl;
    server_name repo.example.com;
    root /srv/repo;
    location / { autoindex on; }
}
```

### Client configuration

Debian 13 (`/etc/apt/sources.list.d/innerstack.list`):
```
deb [trusted=yes] https://repo.example.com/deb/repo trixie main
```
RHEL 10 (`/etc/yum.repos.d/innerstack.repo`):
```ini
[innerstack]
name=InnerStack
baseurl=https://repo.example.com/rpm/el10/$basearch/
enabled=1
gpgcheck=0
```

### Signing (optional)

By default the repo is unsigned and clients use `trusted=yes` / `gpgcheck=0`.
To sign the Debian repo: `REPO_SIGN=1 ./misc/pkg/repo-sync.sh` (or
`GPG_KEY=<keyid>`); clients then switch to
`deb [signed-by=/usr/share/keyrings/innerstack.gpg] ...`.

### Notes

- `repo-sync.sh` is idempotent: re-running re-includes (skips unchanged) and
  `createrepo --update` re-indexes in place.
- To retire a codename, remove it from `gen-map.txt` and run
  `reprepro -b build/deb remove <codename> <pkg>` before re-syncing.

