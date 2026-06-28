# Makefile for the InnerStack project.
#
# Target groups:
#   codegen  : regenerate protobuf/gRPC code (api)
#   binaries : cli, server, indns, ingate, inagent(-slim)
#   package  : deb/rpm packaging and repository assembly
#   utility  : help, clean
#
# Run `make help` to see all available targets.

# Toolchain
PROTOC_CMD         = protoc
HTOML_TAG_FIX_CMD  = htoml-tag-fix
LYNKAPI_FILTER_CMD = lynkapi-fitter

# Protobuf generation arguments
PROTOC_V2_ARGS = --proto_path=./api/inapi \
                 --go_opt=paths=source_relative \
                 --go_out=./pkg/inapi \
                 --go-grpc_out=./pkg/inapi \
                 ./api/inapi/*.proto

PROTOC_AUTH_ARGS = --proto_path=./pkg/inauth \
                   --go_opt=paths=source_relative \
                   --go_out=./pkg/inauth \
                   --go-grpc_out=./pkg/inauth \
                   ./pkg/inauth/inauth.proto

HTOML_TAG_FIX_ARGS      = ./pkg/inapi
HTOML_TAG_FIX_AUTH_ARGS = ./pkg/inauth
LYNKAPI_FILTER_V2_ARGS  = pkg/inapi

# Go build flags
# GOBUILD_ARGS = -trimpath -ldflags="-s -w"
GOBUILD_ARGS    = -trimpath
INAGENT_LDFLAGS = -ldflags="-s -w"

# inagent-slim docker images
INAGENT_SLIM_BASE  = sysinner/innerstack-alpine-inagent-slim:3.23
INAGENT_SLIM_ARCHS = amd64 arm64

# Default target
.PHONY: all
all: api cli server inagent indns ingate
	@echo ""
	@echo "build complete"
	@echo ""

# Code generation
.PHONY: api
api: ## Regenerate protobuf/gRPC code (pkg/inapi, pkg/inauth)
	$(PROTOC_CMD) $(PROTOC_V2_ARGS)
	$(PROTOC_CMD) $(PROTOC_AUTH_ARGS)
	$(HTOML_TAG_FIX_CMD) $(HTOML_TAG_FIX_ARGS)
	$(HTOML_TAG_FIX_CMD) $(HTOML_TAG_FIX_AUTH_ARGS)
	$(LYNKAPI_FILTER_CMD) $(LYNKAPI_FILTER_V2_ARGS)

# Go binaries
.PHONY: cli cli_install server indns ingate
cli: ## Build the innerstack CLI (bin/innerstack)
	go build $(GOBUILD_ARGS) -o bin/innerstack cmd/cli/main.go

cli_install: cli ## Build and install the CLI into $GOPATH/bin
	install bin/innerstack ${GOPATH}/bin/innerstack

server: ## Build the server binary (bin/innerstackd)
	go build $(GOBUILD_ARGS) -o bin/innerstackd cmd/server/main.go

indns: ## Build the embedded DNS server (bin/indnsd)
	go build $(GOBUILD_ARGS) -o bin/indnsd cmd/indns/main.go

ingate: ## Build the HTTP gateway (bin/ingated)
	go build $(GOBUILD_ARGS) -o bin/ingated cmd/ingate/main.go

# inagent (Go, cross-compiled for linux)
.PHONY: inagent inagent-go
inagent: ## Build inagent for linux/amd64 + linux/arm64
	GOOS=linux GOARCH=amd64 go build $(GOBUILD_ARGS) $(INAGENT_LDFLAGS) -o bin/inagent-linux-amd64 cmd/inagent/inagent.go
	GOOS=linux GOARCH=arm64 go build $(GOBUILD_ARGS) $(INAGENT_LDFLAGS) -o bin/inagent-linux-arm64 cmd/inagent/inagent.go

# inagent-go is an alias kept for backward compatibility.
inagent-go: inagent

# inagent-slim (C++ port, built inside docker per arch)
.PHONY: inagent-slim inagent-slim-base inagent-slim-base-amd64 inagent-slim-base-arm64

inagent-slim-base-amd64: ## Build the slim base image for linux/amd64
	docker build --platform linux/amd64 \
		--build-arg TARGETPLATFORM=linux/amd64 \
		-t $(INAGENT_SLIM_BASE)-amd64 -f cmd/inagent-slim/Dockerfile.base cmd/inagent-slim

inagent-slim-base-arm64: ## Build the slim base image for linux/arm64
	docker build --platform linux/arm64 \
		--build-arg TARGETPLATFORM=linux/arm64 \
		-t $(INAGENT_SLIM_BASE)-arm64 -f cmd/inagent-slim/Dockerfile.base cmd/inagent-slim

inagent-slim-base: inagent-slim-base-amd64 inagent-slim-base-arm64 ## Build slim base images for all arches

# Per-arch slim build: ensure the base image exists, then build + extract the
# binary via a builder container.
define INAGENT_SLIM_BUILD
inagent-slim-$(1):
	@docker image inspect $(INAGENT_SLIM_BASE)-$(1) >/dev/null 2>&1 || \
		$(MAKE) inagent-slim-base-$(1)
	docker build --platform linux/$(1) \
		--build-arg TARGETPLATFORM=linux/$(1) \
		--build-arg BUILDER=$(INAGENT_SLIM_BASE)-$(1) \
		-t inagent-slim-builder-$(1) -f cmd/inagent-slim/Dockerfile .
	docker run --rm --platform linux/$(1) \
		-v $(CURDIR)/bin:/output inagent-slim-builder-$(1) \
		cp /build/build/inagent /output/inagent-slim-linux-$(1)
endef

$(foreach arch,$(INAGENT_SLIM_ARCHS),$(eval $(call INAGENT_SLIM_BUILD,$(arch))))

inagent-slim: inagent-slim-amd64 inagent-slim-arm64 ## Build inagent-slim for all arches

# Packaging (nfpm; produces rpm + deb, runs natively on macOS/Linux).
# All builds target the repository layout; --gen places output under
# build/<fmt>/<id>/ (the HTTP server storage path). Initial release: deb13, el10.
.PHONY: deb rpm pkg-all-arch pkg-clean
deb: ## Build DEB packages (deb13, all arches)
	./misc/pkg/build.sh --packager deb --gen deb13 --all-arch

rpm: ## Build RPM packages (el10, all arches)
	./misc/pkg/build.sh --packager rpm --gen el10 --all-arch

pkg-all-arch: deb rpm ## Build all DEB + RPM packages

pkg-clean: ## Remove packaging build artifacts
	./misc/pkg/build.sh --clean

# Repository assembly (per-generation DEB/RPM repos + indexes)
.PHONY: repo repo-images repo-clean
repo: ## Assemble DEB/RPM repositories and indexes
	./misc/pkg/repo-sync.sh

repo-images: ## Build repository container images
	./misc/pkg/repo-sync.sh --build-images

repo-clean: ## Remove assembled repository data
	rm -rf build/deb/repo
	find build/rpm -type d -name repodata -prune -exec rm -rf {} +

# Utility
.PHONY: help
help: ## Show this help message
	@printf "InnerStack - available targets:\n\n"
	@awk 'BEGIN {FS = ":.*?## "} \
		/^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}' \
		$(MAKEFILE_LIST)
