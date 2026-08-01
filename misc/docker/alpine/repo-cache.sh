#!/bin/bash
#
# Alpine apk repository cache — rsync a local mirror into a target directory.
#
# Produces a verbatim rsync copy of the selected Alpine apk repos on disk:
#   $TARGET/alpine/v<ver>/main/<arch>/APKINDEX.tar.gz + *.apk
#   $TARGET/alpine/v<ver>/community/<arch>/...
#
# Signed APKINDEX and .apk files are preserved as-is, so consumers can verify
# packages against /etc/apk/keys shipped in the base image.
#
# Usage:
#   ./repo-cache.sh [target-dir]              sync (default: ./alpine-repo)
#   ./repo-cache.sh /var/lib/alpine-repo      sync into an explicit directory
#   ./repo-cache.sh -h, --help                show this help
#
# Env (optional):
#   ALPINE_VERSIONS   space-separated versions to mirror      (default: 3.23)
#   ALPINE_REPOS      space-separated repos to mirror         (default: "main community")
#   ALPINE_ARCHES     space-separated arches to mirror
#                     (default: detected host arch: x86_64 | aarch64 | armv7)
#   RSYNC_URL         upstream rsync base, trailing slash      (default below)
#   RSYNC_DELETE      1 = prune files removed upstream         (default: 1)
#   RSYNC_EXTRA       extra flags passed through to rsync
#   PROGRESS          1 = show a live single-line transfer bar (default: 1)
#                     set 0 for cron / redirected logs
#
# Upstream rsync sources (override via RSYNC_URL):
#   rsync://rsync.alpinelinux.org/alpine/        (official)
#   rsync://mirrors.tuna.tsinghua.edu.cn/alpine/ (CN)
#   rsync://mirrors.ustc.edu.cn/alpine/          (CN)

set -euo pipefail

DEFAULT_TARGET="$PWD/alpine-repo"
TARGET="${TARGET:-}"
ALPINE_VERSIONS="${ALPINE_VERSIONS:-3.23}"
ALPINE_REPOS="${ALPINE_REPOS:-main community}"
ALPINE_ARCHES="${ALPINE_ARCHES:-}"
RSYNC_URL="${RSYNC_URL:-rsync://rsync.alpinelinux.org/alpine/}"
RSYNC_DELETE="${RSYNC_DELETE:-1}"
RSYNC_EXTRA="${RSYNC_EXTRA:-}"
PROGRESS="${PROGRESS:-1}"

# Filled in main(); declared here so helper functions can read them.
VERSIONS_ARR=()
REPOS_ARR=()
ARCHES_ARR=()

# Cached result of the rsync version probe (see rsync_supports_progress2).
# --info=progress2 only exists in rsync 3.1+; the macOS-bundled 2.6.9 lacks it.
RSYNC_PROGRESS2_OK=""

log_info()  { echo "[INFO] $*"; }
log_warn()  { echo "[WARN] $*" >&2; }
log_error() { echo "[ERROR] $*" >&2; }
die()       { log_error "$@"; exit 1; }

usage() {
    cat <<EOF
Alpine apk repository cache (rsync mirror into a local directory)

Usage: $0 [target-dir]

  target-dir   Where to mirror (default: $DEFAULT_TARGET)

Env (optional):
  ALPINE_VERSIONS   versions to mirror        (default: 3.23)
  ALPINE_REPOS      repos to mirror           (default: "main community")
  ALPINE_ARCHES     arches to mirror          (default: host arch)
  RSYNC_URL         upstream rsync base       (default: official alpine)
  RSYNC_DELETE      1 = prune removed files   (default: 1)
  RSYNC_EXTRA       extra rsync flags
  PROGRESS          1 = live transfer bar     (default: 1; 0 for cron/logs)

Examples:
  $0 /var/lib/alpine-repo
  ALPINE_REPOS="main" $0 /var/lib/alpine-repo
  ALPINE_VERSIONS="3.22 3.23" ALPINE_ARCHES="x86_64 aarch64" $0 /srv/alpine
  RSYNC_URL=rsync://mirrors.tuna.tsinghua.edu.cn/alpine/ $0 /srv/alpine
EOF
}

require() { command -v "$1" >/dev/null 2>&1 || die "$1 not found in PATH ($2)"; }

# --info=progress2 is an rsync 3.1+ feature; older rsync (e.g. macOS 2.6.9)
# errors out on it. Probe the major version once and cache the result.
rsync_supports_progress2() {
    if [[ -z "$RSYNC_PROGRESS2_OK" ]]; then
        local major
        major=$(rsync --version 2>/dev/null \
                 | head -n1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n1 | cut -d. -f1)
        if [[ "$major" =~ ^[0-9]+$ ]] && (( major >= 3 )); then
            RSYNC_PROGRESS2_OK=1
        else
            RSYNC_PROGRESS2_OK=0
        fi
    fi
    [[ "$RSYNC_PROGRESS2_OK" == "1" ]]
}

# Map the running kernel arch to an Alpine apk arch.
detect_arch() {
    case "$(uname -m)" in
        x86_64)         echo x86_64 ;;
        aarch64|arm64)  echo aarch64 ;;
        armv7l)         echo armv7 ;;
        *)              log_warn "unknown arch $(uname -m); defaulting to x86_64"; echo x86_64 ;;
    esac
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -h|--help) usage; exit 0 ;;
            -*)        die "Unknown option: $1 (use -h for help)" ;;
            *)
                [[ -n "$TARGET" ]] && die "only one target-dir allowed: '$TARGET' vs '$1'"
                TARGET="$1"
                ;;
        esac
        shift
    done
    [[ -n "$TARGET" ]] || TARGET="$DEFAULT_TARGET"
}

resolve_config() {
    [[ -n "$RSYNC_URL" ]] || die "RSYNC_URL is empty"
    # rsync module URL must end with a slash so we can append version paths.
    [[ "$RSYNC_URL" == */ ]] || RSYNC_URL="$RSYNC_URL/"

    read -r -a VERSIONS_ARR <<<"$ALPINE_VERSIONS"
    [[ ${#VERSIONS_ARR[@]} -gt 0 ]] || die "ALPINE_VERSIONS is empty"

    read -r -a REPOS_ARR <<<"$ALPINE_REPOS"
    [[ ${#REPOS_ARR[@]} -gt 0 ]] || die "ALPINE_REPOS is empty"

    [[ -n "$ALPINE_ARCHES" ]] || ALPINE_ARCHES="$(detect_arch)"
    read -r -a ARCHES_ARR <<<"$ALPINE_ARCHES"
    [[ ${#ARCHES_ARR[@]} -gt 0 ]] || die "ALPINE_ARCHES is empty"
}

# rsync one version/repo/arch leaf into $TARGET/alpine/v<ver>/<repo>/<arch>/.
# Syncing leaves directly (vs. include/exclude filters) keeps the command
# unambiguous and lets --delete prune each subtree independently.
sync_leaf() {
    local ver="$1" repo="$2" arch="$3"
    local src="${RSYNC_URL}v${ver}/${repo}/${arch}/"
    local dest="$TARGET/alpine/v${ver}/${repo}/${arch}"
    mkdir -p "$dest"

    local rs=(rsync -aHz --partial --stats --contimeout=30 --timeout=300 --no-motd)
    if [[ "$PROGRESS" == "1" ]]; then
        if rsync_supports_progress2; then
            rs+=(--info=progress2)
        else
            rs+=(--progress)
        fi
    fi
    [[ "$RSYNC_DELETE" == "1" ]] && rs+=(--delete)
    if [[ -n "$RSYNC_EXTRA" ]]; then
        # shellcheck disable=SC2086  # intentional word-split of user flags
        rs+=( $RSYNC_EXTRA )
    fi
    rs+=("$src" "$dest/")

    log_info "syncing v${ver}/${repo}/${arch} from ${RSYNC_URL} ..."
    "${rs[@]}" || die "rsync failed for v${ver}/${repo}/${arch} (source: $src)"
}

do_sync() {
    local ver repo arch n=0
    for ver in "${VERSIONS_ARR[@]}"; do
        for repo in "${REPOS_ARR[@]}"; do
            for arch in "${ARCHES_ARR[@]}"; do
                sync_leaf "$ver" "$repo" "$arch"
                n=$((n + 1))
            done
        done
    done
    log_info "synced $n repo tree(s) under $TARGET/alpine"

    # Sanity check: each synced tree must carry an index apk can consume.
    local missing=0 idx
    for ver in "${VERSIONS_ARR[@]}"; do
        for repo in "${REPOS_ARR[@]}"; do
            for arch in "${ARCHES_ARR[@]}"; do
                idx="$TARGET/alpine/v${ver}/${repo}/${arch}/APKINDEX.tar.gz"
                if [[ ! -s "$idx" ]]; then
                    log_warn "missing/empty index: $idx"
                    missing=$((missing + 1))
                fi
            done
        done
    done
    [[ $missing -eq 0 ]] || die "$missing synced tree(s) have no APKINDEX.tar.gz; check RSYNC_URL/versions/arches"

    local napk
    napk=$(find "$TARGET/alpine" -name '*.apk' 2>/dev/null | wc -l | tr -d ' ')
    log_info "mirror holds $napk .apk file(s)"
}

main() {
    parse_args "$@"
    require rsync "mirroring the Alpine repo"

    resolve_config

    log_info "target:      $TARGET"
    log_info "versions:    ${VERSIONS_ARR[*]}"
    log_info "repos:       ${REPOS_ARR[*]}"
    log_info "arches:      ${ARCHES_ARR[*]}"
    log_info "upstream:    $RSYNC_URL"
    log_info "delete:      $RSYNC_DELETE   (RSYNC_DELETE)"

    mkdir -p "$TARGET"
    do_sync
    log_info "done. mirror at $TARGET/alpine"
}

main "$@"
