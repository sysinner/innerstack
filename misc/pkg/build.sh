#!/bin/bash
#
# InnerStack packaging script (nfpm-based)
#
# Cross-compiles Go binaries for linux/amd64(+arm64) and assembles them into
# .rpm / .deb packages using nfpm. nfpm is a single static binary that runs
# natively on macOS and Linux, so no rpmbuild / Docker is required.
#
# Usage:
#   ./misc/pkg/build.sh [options]
#
# Options:
#   --arch <amd64|arm64>     Target architecture (default: host arch)
#   --all-arch               Build for both amd64 and arm64
#   --with-ingate            Include ingate subpackage (default: yes)
#   --without-ingate         Exclude ingate subpackage
#   --with-indns             Include indns subpackage (default: yes)
#   --without-indns          Exclude indns subpackage
#   --with-cli               Include cli subpackage (default: yes)
#   --without-cli            Exclude cli subpackage
#   --with-inagent-slim      Include inagent-slim (C++) binary (default: yes;
#                            requires Docker to build)
#   --without-inagent-slim   Exclude inagent-slim binary (no Docker needed)
#   --packager <rpm|deb>     Package format (default: both)
#   --gen <id>               (required) generation id: tags release with +<id>,
#                            places output at build/deb/<id>/ or
#                            build/rpm/<id>/<arch>/ (= HTTP server storage path)
#   --version <ver>          Package version (default: git tag or "2.0.0")
#   --clean                  Clean build artifacts and exit
#   -h, --help               Show this help message
#

set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PKG_DIR="$SCRIPT_DIR"

# Final package output root; rpm -> build/rpm, deb -> build/deb.
OUT_DIR="$PROJECT_ROOT/build"

DEFAULT_VERSION="2.0.0"
# Array so "-s -w" stays a single ldflags value under word splitting.
GOBUILD_ARGS=(-trimpath -ldflags "-s -w")

# Component toggles
WITH_INGATE="yes"
WITH_INDNS="yes"
WITH_CLI="yes"
WITH_INAGENT_SLIM="yes"

# Target architectures
TARGET_ARCHES=()

# Package formats
PACKAGERS=("rpm" "deb")

# Misc flags
CLEAN_ONLY=NO
VERSION=""

# Generation id (required): tags a set of ABI-compatible distros (e.g. deb13,
# el10). "+<id>" is appended to the package release field and packages are
# placed directly into the repository layout, which is the HTTP server storage
# path (rsync build/deb and build/rpm to the server):
#   deb -> build/deb/<id>/         rpm -> build/rpm/<id>/<arch>/
# The upstream version is left untouched, so subpackage Depends pins stay valid.
GEN_ID=""
RELEASE_SUFFIX=""    # derived: "+<id>"

# ---------------------------------------------------------------------------
# Helper Functions
# ---------------------------------------------------------------------------
print_usage() {
    cat <<EOF
InnerStack packaging script (nfpm)

Usage: $0 [options]

Options:
  --arch <amd64|arm64>     Target architecture (default: host arch)
  --all-arch               Build for both amd64 and arm64
  --with-ingate            Include ingate subpackage (default: yes)
  --without-ingate         Exclude ingate subpackage
  --with-indns             Include indns subpackage (default: yes)
  --without-indns          Exclude indns subpackage
  --with-cli               Include cli subpackage (default: yes)
  --without-cli            Exclude cli subpackage
  --with-inagent-slim      Include inagent-slim (C++) binary (default: yes;
                           requires Docker to build)
  --without-inagent-slim   Exclude inagent-slim binary (no Docker needed)
  --packager <rpm|deb>     Package format (default: both)
  --gen <id>               (required) generation id; output build/deb/<id>/ or build/rpm/<id>/<arch>/
  --version <ver>          Package version (default: $DEFAULT_VERSION)
  --clean                  Clean build artifacts and exit
  -h, --help               Show this help message

Examples:
  $0 --packager deb --gen deb13 --all-arch   # Debian 13 repo (both arches)
  $0 --packager rpm --gen el10  --all-arch   # RHEL 10 repo (both arches)
  $0 --packager deb --gen deb13 --arch arm64 # single arch
  $0 --clean

Prerequisites:
  go   (Go toolchain >= 1.26)
  nfpm (https://nfpm.goreleaser.com/install/)
       macOS:   brew install nfpm
       Linux:   go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
  docker + make (only when --with-inagent-slim, the default)
EOF
}

log_info()  { echo "[INFO] $*"; }
log_warn()  { echo "[WARN] $*" >&2; }
log_error() { echo "[ERROR] $*" >&2; }
die()       { log_error "$@"; exit 1; }

detect_host_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *)             echo "amd64" ;;
    esac
}

# ---------------------------------------------------------------------------
# Parse Arguments
# ---------------------------------------------------------------------------
parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --arch)
                shift
                TARGET_ARCHES+=("$1")
                ;;
            --all-arch)
                TARGET_ARCHES=("amd64" "arm64")
                ;;
            --with-ingate)  WITH_INGATE="yes" ;;
            --without-ingate) WITH_INGATE="no" ;;
            --with-indns)   WITH_INDNS="yes" ;;
            --without-indns)  WITH_INDNS="no" ;;
            --with-cli)     WITH_CLI="yes" ;;
            --without-cli)  WITH_CLI="no" ;;
            --with-inagent-slim)  WITH_INAGENT_SLIM="yes" ;;
            --without-inagent-slim) WITH_INAGENT_SLIM="no" ;;
            --packager)
                shift
                case "$1" in
                    rpm|deb) PACKAGERS=("$1") ;;
                    *) die "Invalid --packager value: $1 (use rpm or deb)" ;;
                esac
                ;;
            --gen)
                shift
                GEN_ID="$1"
                ;;
            --version)
                shift
                VERSION="$1"
                ;;
            --clean)
                CLEAN_ONLY=YES
                ;;
            -h|--help)
                print_usage
                exit 0
                ;;
            *)
                die "Unknown option: $1 (use -h for help)"
                ;;
        esac
        shift
    done

    if [[ ${#TARGET_ARCHES[@]} -eq 0 ]]; then
        TARGET_ARCHES=($(detect_host_arch))
    fi

    if [[ -z "$VERSION" ]]; then
        VERSION=$(cd "$PROJECT_ROOT" && git describe --tags --always 2>/dev/null || echo "$DEFAULT_VERSION")
        VERSION="${VERSION#v}"
    fi

    # Derive the release suffix from the generation id.
    if [[ -n "$GEN_ID" ]]; then
        RELEASE_SUFFIX="+$GEN_ID"
    else
        RELEASE_SUFFIX=""
    fi
}

# ---------------------------------------------------------------------------
# Prerequisites
# ---------------------------------------------------------------------------
check_prerequisites() {
    command -v go >/dev/null 2>&1 || die "go compiler not found in PATH"
    command -v nfpm >/dev/null 2>&1 || die "nfpm not found in PATH (see https://nfpm.goreleaser.com/install/)"
    if [[ "$WITH_INAGENT_SLIM" == "yes" ]]; then
        command -v docker >/dev/null 2>&1 \
            || die "docker not found in PATH (required to build inagent-slim; use --without-inagent-slim to skip)"
        command -v make >/dev/null 2>&1 \
            || die "make not found in PATH (required to build inagent-slim)"
    fi
}

# ---------------------------------------------------------------------------
# Build Functions
# ---------------------------------------------------------------------------
go_build() {
    local arch="$1" pkg="$2" out="$3"
    log_info "  building $out (linux/$arch) ..."
    GOOS=linux GOARCH="$arch" CGO_ENABLED=0 \
        go build "${GOBUILD_ARGS[@]}" -o "$out" "$pkg"
}

# Build the Go inagent for a single target arch.
build_inagent_go() {
    local arch="$1"
    [[ -f "$PROJECT_ROOT/bin/inagent-linux-$arch" ]] && return
    go_build "$arch" "$PROJECT_ROOT/cmd/inagent/inagent.go" \
        "$PROJECT_ROOT/bin/inagent-linux-$arch"
}

# Build the C++ inagent-slim for a single target arch (Docker-based).
build_inagent_slim() {
    local arch="$1"
    [[ -f "$PROJECT_ROOT/bin/inagent-slim-linux-$arch" ]] && return
    log_info "  building inagent-slim-linux-$arch (docker) ..."
    (cd "$PROJECT_ROOT" && make "inagent-slim-$arch")
}

build_arch_binaries() {
    local arch="$1"
    go_build "$arch" "$PROJECT_ROOT/cmd/server/main.go" \
        "$PROJECT_ROOT/bin/innerstackd-linux-$arch"
    build_inagent_go "$arch"
    if [[ "$WITH_INAGENT_SLIM" == "yes" ]]; then
        build_inagent_slim "$arch"
    fi
    if [[ "$WITH_INGATE" == "yes" ]]; then
        go_build "$arch" "$PROJECT_ROOT/cmd/ingate/main.go" \
            "$PROJECT_ROOT/bin/ingated-linux-$arch"
    fi
    if [[ "$WITH_INDNS" == "yes" ]]; then
        go_build "$arch" "$PROJECT_ROOT/cmd/indns/main.go" \
            "$PROJECT_ROOT/bin/indnsd-linux-$arch"
    fi
    if [[ "$WITH_CLI" == "yes" ]]; then
        go_build "$arch" "$PROJECT_ROOT/cmd/cli/main.go" \
            "$PROJECT_ROOT/bin/innerstack-linux-$arch"
    fi
}

# ---------------------------------------------------------------------------
# Staging
# ---------------------------------------------------------------------------
# Assemble the tree nfpm reads from. Uses absolute paths injected via ${STAGE}.
stage_arch() {
    local arch="$1"
    local stage="$2"

    log_info "Staging artifacts for $arch -> $stage"
    rm -rf "$stage"
    mkdir -p "$stage/bin" "$stage/systemd" "$stage/empty"

    # binaries
    cp "$PROJECT_ROOT/bin/innerstackd-linux-$arch" "$stage/bin/innerstackd"
    cp "$PROJECT_ROOT/bin/inagent-linux-$arch"     "$stage/bin/inagent-linux-$arch"
    [[ "$WITH_INAGENT_SLIM" == "yes" ]] && \
        cp "$PROJECT_ROOT/bin/inagent-slim-linux-$arch" "$stage/bin/inagent-slim-linux-$arch"
    [[ "$WITH_INGATE" == "yes" ]] && \
        cp "$PROJECT_ROOT/bin/ingated-linux-$arch" "$stage/bin/ingated"
    [[ "$WITH_INDNS" == "yes" ]] && \
        cp "$PROJECT_ROOT/bin/indnsd-linux-$arch"  "$stage/bin/indnsd"
    [[ "$WITH_CLI" == "yes" ]] && \
        cp "$PROJECT_ROOT/bin/innerstack-linux-$arch" "$stage/bin/innerstack"

    # container guest init script
    cp "$PROJECT_ROOT/internal/hostlet/scripts/ininit" "$stage/ininit"

    # container base-image Dockerfile templates, shipped under
    # /opt/innerstack/misc/docker/<distro>/ mirroring the source tree.
    mkdir -p "$stage/misc/docker"
    cp -R "$PROJECT_ROOT/misc/docker/alpine" "$PROJECT_ROOT/misc/docker/debian" "$stage/misc/docker/"

    # systemd units (colocated with each binary under cmd/<name>/)
    cp "$PROJECT_ROOT/cmd/server/innerstack.service" "$stage/systemd/innerstack.service"
    [[ "$WITH_INGATE" == "yes" ]] && \
        cp "$PROJECT_ROOT/cmd/ingate/systemd.service" "$stage/systemd/ingate.service"
    [[ "$WITH_INDNS" == "yes" ]] && \
        cp "$PROJECT_ROOT/cmd/indns/systemd.service" "$stage/systemd/indns.service"

    # config files (empty placeholders, except indnsd which ships defaults)
    : > "$stage/empty/innerstack.toml"
    [[ "$WITH_INGATE" == "yes" ]] && : > "$stage/empty/ingate.toml"
    if [[ "$WITH_INDNS" == "yes" ]]; then
        cp "$PROJECT_ROOT/cmd/indns/indnsd.default.toml" "$stage/empty/indnsd.toml"
    fi
}

# ---------------------------------------------------------------------------
# nfpm packaging
# ---------------------------------------------------------------------------
run_nfpm() {
    local pkg_yaml="$1" arch="$2" stage="$3"

    # The main package yaml embeds an optional inagent-slim content block
    # between marker comments. When slim is disabled, strip that block into a
    # generated yaml so nfpm does not reference an unstaged binary.
    local eff_yaml="$pkg_yaml"
    if [[ "$WITH_INAGENT_SLIM" != "yes" ]] && grep -q '# >>> inagent-slim >>>' "$pkg_yaml"; then
        local gen_dir="$PROJECT_ROOT/build/pkg-yaml"
        mkdir -p "$gen_dir"
        eff_yaml="$gen_dir/$(basename "$pkg_yaml" .yaml)-noslim-$arch.yaml"
        sed '/# >>> inagent-slim >>>/,/# <<< inagent-slim <<</d' "$pkg_yaml" > "$eff_yaml"
    fi

    # nfpm resolves relative content/script paths against CWD, so run from the
    # packaging dir where scripts/ lives. Binary/unit/config paths use absolute
    # ${STAGE} via env expansion (expand: true in the yaml).
    (
        cd "$PKG_DIR"
        for fmt in "${PACKAGERS[@]}"; do
            log_info "  nfpm pkg: $(basename "$eff_yaml") [$fmt, linux/$arch]"
            # Place directly into the repository layout (the HTTP server storage
            # path): deb -> build/deb/<gen>/, rpm -> build/rpm/<gen>/<arch>/.
            local outdir
            if [[ "$fmt" == "deb" ]]; then
                outdir="$OUT_DIR/deb/$GEN_ID"
            else
                local rpmarch
                case "$arch" in amd64) rpmarch=x86_64 ;; arm64) rpmarch=aarch64 ;; *) rpmarch="$arch" ;; esac
                outdir="$OUT_DIR/rpm/$GEN_ID/$rpmarch"
            fi
            mkdir -p "$outdir"
            VERSION="$VERSION" ARCH="$arch" STAGE="$stage" RELEASE_SUFFIX="$RELEASE_SUFFIX" \
                nfpm package --config "$eff_yaml" --packager "$fmt" --target "$outdir/"
        done
    )
}

package_arch() {
    local arch="$1"
    local stage="$PROJECT_ROOT/bin/pkg-stage-$arch"

    stage_arch "$arch" "$stage"

    run_nfpm "$PKG_DIR/nfpm.yaml" "$arch" "$stage"
    if [[ "$WITH_INGATE" == "yes" ]]; then run_nfpm "$PKG_DIR/nfpm.ingate.yaml" "$arch" "$stage"; fi
    if [[ "$WITH_INDNS" == "yes" ]];  then run_nfpm "$PKG_DIR/nfpm.indns.yaml"  "$arch" "$stage"; fi
    if [[ "$WITH_CLI" == "yes" ]];    then run_nfpm "$PKG_DIR/nfpm.cli.yaml"    "$arch" "$stage"; fi
}

# ---------------------------------------------------------------------------
# Clean
# ---------------------------------------------------------------------------
clean_artifacts() {
    log_info "Cleaning build artifacts ..."
    rm -rf "$PROJECT_ROOT/bin/pkg-stage-"*
    rm -rf "$OUT_DIR"
    rm -f  "$PROJECT_ROOT/bin/innerstackd-linux-"*
    rm -f  "$PROJECT_ROOT/bin/ingated-linux-"*
    rm -f  "$PROJECT_ROOT/bin/indnsd-linux-"*
    rm -f  "$PROJECT_ROOT/bin/innerstack-linux-"*
    rm -f  "$PROJECT_ROOT/bin/inagent-linux-"*
    rm -f  "$PROJECT_ROOT/bin/inagent-slim-linux-"*
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
main() {
    parse_args "$@"

    if [[ "$CLEAN_ONLY" == "YES" ]]; then
        clean_artifacts
        exit 0
    fi

    # All packaging targets the repository layout, so a generation id is required.
    [[ -n "$GEN_ID" ]] \
        || die "--gen <id> is required (e.g. --gen deb13 for deb, --gen el10 for rpm); packages go to build/<fmt>/<id>/"

    check_prerequisites

    local summary_arches
    summary_arches=$(IFS=,; echo "${TARGET_ARCHES[*]}")
    local summary_fmts
    summary_fmts=$(IFS=,; echo "${PACKAGERS[*]}")

    echo ""
    log_info "InnerStack packaging configuration:"
    echo "  Version:      $VERSION"
    echo "  Generation:   ${GEN_ID:-none}"
    echo "  Target Arch:  $summary_arches"
    echo "  Format(s):    $summary_fmts"
    echo "  Components:"
    echo "    innerstack (main) + inagent : yes (always)"
    echo "    inagent-slim                : $WITH_INAGENT_SLIM"
    echo "    ingate                      : $WITH_INGATE"
    echo "    indns                       : $WITH_INDNS"
    echo "    cli                         : $WITH_CLI"
    echo ""

    cd "$PROJECT_ROOT"

    for arch in "${TARGET_ARCHES[@]}"; do
        log_info "=========================================="
        log_info "Building for target: linux/$arch"
        log_info "=========================================="
        build_arch_binaries "$arch"
        package_arch "$arch"
    done

    echo ""
    log_info "Packaging complete!"
    local fmt basedir
    for fmt in "${PACKAGERS[@]}"; do
        basedir="$OUT_DIR/$fmt/$GEN_ID"
        log_info "$(printf '%s' "$fmt" | tr '[:lower:]' '[:upper:]') artifacts in: $basedir/"
        find "$basedir" -type f \( -name "*.deb" -o -name "*.rpm" \) 2>/dev/null | sed 's/^/  /' || true
    done
}

main "$@"
