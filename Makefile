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

.PHONY: api cli server inagent indns ingate
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
	go build $(GOBUILD_ARGS) -o bin/instack cmd/cli/main.go

server:
	go build $(GOBUILD_ARGS) -o bin/instackd cmd/server/main.go

indns:
	go build $(GOBUILD_ARGS) -o bin/indnsd cmd/indns/main.go

ingate:
	go build $(GOBUILD_ARGS) -o bin/ingated cmd/ingate/main.go

inagent:
	GOOS=linux GOARCH=amd64 go build $(GOBUILD_ARGS) -o bin/inagent-linux-amd64 cmd/inagent/inagent.go
	GOOS=linux GOARCH=arm64 go build $(GOBUILD_ARGS) -o bin/inagent-linux-arm64 cmd/inagent/inagent.go

