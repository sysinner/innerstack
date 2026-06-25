PROTOC_CMD = protoc

PROTOC_V2_ARGS = --proto_path=./api/inapi --go_opt=paths=source_relative --go_out=./pkg/inapi --go-grpc_out=./pkg/inapi ./api/inapi/*.proto
PROTOC_AUTH_ARGS = --proto_path=./pkg/inauth --go_opt=paths=source_relative --go_out=./pkg/inauth --go-grpc_out=./pkg/inauth ./pkg/inauth/inauth.proto

HTOML_TAG_FIX_CMD = htoml-tag-fix
HTOML_TAG_FIX_ARGS = ./pkg/inapi
HTOML_TAG_FIX_AUTH_ARGS = ./pkg/inauth

LYNKAPI_FILTER_CMD = lynkapi-fitter
LYNKAPI_FILTER_V2_ARGS = pkg/inapi

# GOBUILD_ARGS = -trimpath -ldflags="-s -w"
GOBUILD_ARGS = -trimpath

.PHONY: api cli cli_install server inagent indns ingate
all: api cli server inagent indns ingate
	@echo ""
	@echo "build complete"
	@echo ""

api:
	$(PROTOC_CMD) $(PROTOC_V2_ARGS)
	$(PROTOC_CMD) $(PROTOC_AUTH_ARGS)
	$(HTOML_TAG_FIX_CMD) $(HTOML_TAG_FIX_ARGS)
	$(HTOML_TAG_FIX_CMD) $(HTOML_TAG_FIX_AUTH_ARGS)
	$(LYNKAPI_FILTER_CMD) $(LYNKAPI_FILTER_V2_ARGS)

cli:
	go build $(GOBUILD_ARGS) -o bin/innerstack cmd/cli/main.go

cli_install: cli
	install bin/innerstack ${GOPATH}/bin/innerstack

server:
	go build $(GOBUILD_ARGS) -o bin/innerstackd cmd/server/main.go

indns:
	go build $(GOBUILD_ARGS) -o bin/indnsd cmd/indns/main.go

ingate:
	go build $(GOBUILD_ARGS) -o bin/ingated cmd/ingate/main.go

inagent:
	GOOS=linux GOARCH=amd64 go build $(GOBUILD_ARGS) -ldflags="-s -w" -o bin/inagent-linux-amd64 cmd/inagent/inagent.go
	GOOS=linux GOARCH=arm64 go build $(GOBUILD_ARGS) -ldflags="-s -w" -o bin/inagent-linux-arm64 cmd/inagent/inagent.go

inagent-go:
	GOOS=linux GOARCH=amd64 go build $(GOBUILD_ARGS) -ldflags="-s -w" -o bin/inagent-linux-amd64 cmd/inagent/inagent.go
	GOOS=linux GOARCH=arm64 go build $(GOBUILD_ARGS) -ldflags="-s -w" -o bin/inagent-linux-arm64 cmd/inagent/inagent.go

INAGENT_CPP_BASE = sysinner/innerstack-alpine-inagent-cpp:3.23

.PHONY: inagent-cpp-base inagent-cpp-base-amd64 inagent-cpp-base-arm64

inagent-cpp-base-amd64:
	docker build --platform linux/amd64 \
		--build-arg TARGETPLATFORM=linux/amd64 \
		-t $(INAGENT_CPP_BASE)-amd64 -f cmd/inagent-cpp/Dockerfile.base cmd/inagent-cpp

inagent-cpp-base-arm64:
	docker build --platform linux/arm64 \
		--build-arg TARGETPLATFORM=linux/arm64 \
		-t $(INAGENT_CPP_BASE)-arm64 -f cmd/inagent-cpp/Dockerfile.base cmd/inagent-cpp

inagent-cpp-base: inagent-cpp-base-amd64 inagent-cpp-base-arm64

INAGENT_CPP_ARCHS = amd64 arm64

define INAGENT_CPP_BUILD
inagent-cpp-$(1):
	@docker image inspect $(INAGENT_CPP_BASE)-$(1) >/dev/null 2>&1 || \
		$(MAKE) inagent-cpp-base-$(1)
	docker build --platform linux/$(1) \
		--build-arg TARGETPLATFORM=linux/$(1) \
		--build-arg BUILDER=$(INAGENT_CPP_BASE)-$(1) \
		-t inagent-cpp-builder-$(1) -f cmd/inagent-cpp/Dockerfile .
	docker run --rm --platform linux/$(1) \
		-v $(CURDIR)/bin:/output inagent-cpp-builder-$(1) \
		cp /build/build/inagent /output/inagent-cpp-linux-$(1)
endef

$(foreach arch,$(INAGENT_CPP_ARCHS),$(eval $(call INAGENT_CPP_BUILD,$(arch))))

inagent-cpp: inagent-cpp-amd64 inagent-cpp-arm64

# Packaging targets (nfpm; produces rpm + deb, runs natively on macOS/Linux).
# All builds target the repository layout; --gen places output under
# build/<fmt>/<id>/ (the HTTP server storage path). Initial release: deb13, el10.
deb:
	./misc/pkg/build.sh --packager deb --gen deb13 --all-arch

rpm:
	./misc/pkg/build.sh --packager rpm --gen el10 --all-arch

pkg-all-arch: deb rpm

pkg-clean:
	./misc/pkg/build.sh --clean

# Repository targets (assemble per-generation DEB/RPM repos + indexes)
repo:
	./misc/pkg/repo-sync.sh

repo-images:
	./misc/pkg/repo-sync.sh --build-images

repo-clean:
	rm -rf build/deb/repo
	find build/rpm -type d -name repodata -prune -exec rm -rf {} +

