#!/bin/bash
#
# InnerStack repository sync — create repo indexes for the packages that
# build.sh already placed under build/deb/ and build/rpm/. No remote upload.
#
# Packages land directly in the repository layout (build.sh --gen):
#   deb: build/deb/<gen>/*.deb         -> reprepro builds pool/dists under build/deb/repo/
#   rpm: build/rpm/<gen>/<arch>/*.rpm  -> createrepo indexes each <arch>/ in place
#
# Distro-specific indexers run inside local builder Docker images, so the host
# needs only bash + docker (no reprepro / createrepo installed locally):
#   - Debian (reprepro)     -> innerstack-repo-deb   (Debian + reprepro)
#   - RHEL   (createrepo_c) -> innerstack-repo-el:10 (Rocky 10 + createrepo_c)
#
# This script is dual-purpose:
#   ./repo-sync.sh              host: ensure images, run both sides in containers
#   ./repo-sync.sh --debian     inside the debian image: build the Debian repo
#   ./repo-sync.sh --rhel       inside the rocky image:   build the RHEL repos
#   ./repo-sync.sh --build-images  (re)build the builder images and exit
#
# Env (optional, Debian signing):
#   REPO_SIGN=1        sign the Debian repo (InRelease / Release.gpg)
#   GPG_KEY=<keyid>    key id to sign with (implies REPO_SIGN=1)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PKG_DIR="$SCRIPT_DIR"

GEN_MAP="$PKG_DIR/gen-map.txt"
OUT_DIR="$PROJECT_ROOT/build"
DEB_BASE="$OUT_DIR/deb"    # holds incoming <gen>/ + the reprepro repo (repo/)
DEB_DIR="$DEB_BASE/repo"   # reprepro basedir: conf/ pool/ dists/ db/
RPM_DIR="$OUT_DIR/rpm"     # per-gen dirs: <gen>/<arch>/ with repodata/

# Builder images + their Dockerfiles (built locally; see --build-images).
IMG_DEB="innerstack-repo-deb:latest"
IMG_EL="innerstack-repo-el:10"
DF_DIR="$PKG_DIR/docker"
DF_DEB="$DF_DIR/Dockerfile.repo-deb"
DF_EL="$DF_DIR/Dockerfile.repo-el"

# Execution mode.
MODE="all"   # all | debian | rhel | build-images

log_info()  { echo "[INFO] $*"; }
log_warn()  { echo "[WARN] $*" >&2; }
log_error() { echo "[ERROR] $*" >&2; }
die()       { log_error "$@"; exit 1; }

usage() {
    cat <<EOF
InnerStack repository sync (writes indexes under $DEB_DIR and $RPM_DIR)

Usage: $0 [options]

Options:
  --build-images     (Re)build the local builder Docker images and exit
  -h, --help         Show this help message

Internal (used when run inside a builder container):
  --debian           Build only the Debian repo (reprepro)
  --rhel             Build only the RHEL repos (createrepo)

Env (optional, Debian signing):
  REPO_SIGN=1        Sign the Debian repo
  GPG_KEY=<id>       Key id to sign with (implies REPO_SIGN=1)

Builder images:
  $IMG_DEB  <- $DF_DEB
  $IMG_EL <- $DF_EL
EOF
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --build-images) MODE="build-images" ;;
            --debian)       MODE="debian" ;;
            --rhel)         MODE="rhel" ;;
            -h|--help)      usage; exit 0 ;;
            *) die "Unknown option: $1 (use -h for help)" ;;
        esac
        shift
    done
}

require() { command -v "$1" >/dev/null 2>&1 || die "$1 not found in PATH ($2)"; }

# ---------------------------------------------------------------------------
# Host: builder image management + container invocation
# ---------------------------------------------------------------------------
ensure_image() {
    local img="$1" df="$2"; shift 2   # remaining args -> docker build flags
    if [[ "$MODE" != "build-images" ]]; then
        if docker image inspect "$img" >/dev/null 2>&1; then return 0; fi
        log_info "builder image $img not found; building ..."
    else
        log_info "(re)building builder image $img ..."
    fi
    [[ -f "$df" ]] || die "Dockerfile not found: $df"
    docker build -t "$img" -f "$df" "$DF_DIR" "$@" \
        || die "failed to build $img (see docker output above)"
}

run_side() {
    local img="$1" mode="$2"
    log_info "running $mode side in $img ..."
    local run_args=(
        --rm
        -v "$PROJECT_ROOT/build":/work/build
        -v "$PROJECT_ROOT/misc/pkg":/work/misc/pkg
        -e HOME=/tmp
        -e REPO_SIGN="${REPO_SIGN:-}"
        -e GPG_KEY="${GPG_KEY:-}"
        --user "$(id -u):$(id -g)"
        -w /work
    )
    # When signing, mount the host GPG keyring into the container.
    if [[ "${REPO_SIGN:-}" == "1" || -n "${GPG_KEY:-}" ]]; then
        local gh="${GNUPGHOME:-$HOME/.gnupg}"
        [[ -d "$gh" ]] || die "signing requested but no GPG keyring at $gh"
        run_args+=(-v "$gh":/gnupg -e GNUPGHOME=/gnupg)
    fi
    docker run "${run_args[@]}" "$img" bash /work/misc/pkg/repo-sync.sh "$mode"
}

# ---------------------------------------------------------------------------
# Debian (reprepro) — runs inside the debian builder container
# ---------------------------------------------------------------------------
sync_debian() {
    require reprepro "Debian repo indexing (image: $IMG_DEB)"
    [[ -f "$GEN_MAP" ]] || die "generation map not found: $GEN_MAP"

    # Parse gen-map into "codename|gen" tokens plus a distinct-gen list.
    # Index-free iteration only (no C-style for / array indexing), so the logic
    # is unambiguous and portable across bash versions.
    local entries=() all_gens=()
    local line_cn line_gen rest
    while read -r line_cn line_gen rest; do
        [[ -z "$line_cn" || "${line_cn:0:1}" == "#" ]] && continue
        [[ -z "$line_gen" ]] && continue
        entries+=("$line_cn|$line_gen")
        # collect distinct generation ids. Guard the iteration: "${arr[@]}" on
        # an empty array errors under `set -u` on older bash; ${#arr[@]} is safe.
        local seen=0 gg
        if [[ ${#all_gens[@]} -gt 0 ]]; then
            for gg in "${all_gens[@]}"; do [[ "$gg" == "$line_gen" ]] && { seen=1; break; }; done
        fi
        [[ $seen -eq 0 ]] && all_gens+=("$line_gen")
    done < "$GEN_MAP"

    [[ ${#entries[@]} -gt 0 ]] || { log_warn "no codenames in $GEN_MAP; skipping Debian"; return 0; }

    local sign_with=""
    if [[ "${REPO_SIGN:-}" == "1" || -n "${GPG_KEY:-}" ]]; then
        sign_with="${GPG_KEY:-default}"
        require gpg "repository signing"
    fi

    # Generate conf/distributions: one Codename stanza per codename (file order).
    local conf_dir="$DEB_DIR/conf"
    mkdir -p "$conf_dir"
    local tok cn
    {
        for tok in "${entries[@]}"; do
            cn="${tok%|*}"
            cat <<EOF
Origin: SysInner
Label: InnerStack
Codename: $cn
Architectures: amd64 arm64 source
Components: main
Description: InnerStack packages for Debian $cn
EOF
            [[ -n "$sign_with" ]] && echo "SignWith: $sign_with"
            echo
        done
    } > "$conf_dir/distributions"
    log_info "wrote $conf_dir/distributions (${#entries[@]} codename(s))"

    # Record each generation's debs into every codename of that generation.
    # Incoming debs live at $DEB_DIR/<gen>/ (placed there by build.sh --gen).
    local g
    for g in "${all_gens[@]}"; do
        local debdir="$DEB_BASE/$g"
        # Guard the find: under `set -o pipefail`, `find` on a missing dir
        # returns non-zero, and assigning that command substitution would abort
        # the script via `set -e`. Check the directory first.
        if [[ ! -d "$debdir" ]]; then
            log_warn "no debs for generation '$g' (expected $debdir/*.deb); its codenames stay empty"
            continue
        fi
        local ndeb
        ndeb=$(find "$debdir" -maxdepth 1 -name '*.deb' | wc -l | tr -d ' ')
        if [[ "$ndeb" -eq 0 ]]; then
            log_warn "generation '$g' has no .deb in $debdir; its codenames stay empty"
            continue
        fi
        local debs=("$debdir"/*.deb)
        local cns=()
        for tok in "${entries[@]}"; do
            [[ "${tok#*|}" == "$g" ]] && cns+=("${tok%|*}")
        done
        log_info "generation '$g' -> codenames: ${cns[*]} (${#debs[@]} deb file(s))"
        local cn_
        for cn_ in "${cns[@]}"; do
            reprepro -b "$DEB_DIR" includedeb "$cn_" "${debs[@]}"
        done
    done

    reprepro -b "$DEB_DIR" check

    log_info "Debian pool contents:"
    find "$DEB_DIR/pool" -name '*.deb' 2>/dev/null | sed 's/^/  /' || true
    for tok in "${entries[@]}"; do
        cn="${tok%|*}"
        log_info "Debian [$cn] references:"
        reprepro -b "$DEB_DIR" list "$cn" | sed 's/^/    /'
    done
}

# ---------------------------------------------------------------------------
# RHEL (createrepo) — runs inside the rocky builder container
#
# Indexes every real (non-symlink) generation dir under build/rpm/ in place.
# A future ABI-compatible EL release reuses an existing generation via a
# symlink, e.g.  ln -s el10 build/rpm/el11  — it inherits el10's rpms and
# repodata, so this loop skips symlinks.
# ---------------------------------------------------------------------------
sync_rhel() {
    local createrepo
    if   command -v createrepo_c >/dev/null 2>&1; then createrepo=createrepo_c
    elif command -v createrepo   >/dev/null 2>&1; then createrepo=createrepo
    else die "createrepo not found in PATH (image: $IMG_EL)"; fi

    [[ -d "$RPM_DIR" ]] || { log_warn "no $RPM_DIR directory; skipping RHEL"; return 0; }

    local gendir
    for gendir in "$RPM_DIR"/*/; do
        [[ -d "$gendir" ]] || continue
        local gen; gen="$(basename "${gendir%/}")"
        if [[ -L "${gendir%/}" ]]; then
            log_info "RHEL gen '$gen' is a symlink; skipping (inherits target's rpms+repodata)"
            continue
        fi
        # createrepo each arch subdir in place; --update is safe to re-run.
        local adir
        for adir in "$gendir"*/; do
            [[ -d "$adir" ]] || continue
            if [[ -d "${adir}repodata" ]]; then
                "$createrepo" --update "$adir"
            else
                "$createrepo" "$adir"
            fi
            local n arch
            arch="$(basename "${adir%/}")"
            n=$(find "${adir}" -maxdepth 1 -name '*.rpm' | wc -l | tr -d ' ')
            log_info "RHEL $gen/$arch: $n rpm(s), repodata ready"
        done
    done
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
main() {
    parse_args "$@"

    case "$MODE" in
        debian)        mkdir -p "$DEB_DIR"; sync_debian; exit 0 ;;   # inside debian container
        rhel)          mkdir -p "$RPM_DIR"; sync_rhel;   exit 0 ;;   # inside rocky container
        build-images)
            require docker "building images"
            ensure_image "$IMG_DEB" "$DF_DEB" --build-arg "APT_MIRROR=${APT_MIRROR:-deb.debian.org}"
            ensure_image "$IMG_EL"  "$DF_EL"
            log_info "builder images ready."
            exit 0 ;;
    esac

    # host orchestration (MODE=all)
    require docker "running builder containers"
    mkdir -p "$DEB_DIR" "$RPM_DIR"
    log_info "repo roots: $DEB_DIR (deb), $RPM_DIR (rpm)"
    ensure_image "$IMG_DEB" "$DF_DEB" --build-arg "APT_MIRROR=${APT_MIRROR:-deb.debian.org}"
    ensure_image "$IMG_EL"  "$DF_EL"
    run_side "$IMG_DEB" --debian
    run_side "$IMG_EL"  --rhel
    echo ""
    log_info "Repository sync complete."
    log_info "Serve over HTTP, e.g.:  cd $OUT_DIR && python3 -m http.server 8080"
}

main "$@"
