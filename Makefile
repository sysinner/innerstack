PROTOC_CMD = protoc

PROTOC_V2_ARGS = --proto_path=./api/inapi --go_opt=paths=source_relative --go_out=./inapi --go-grpc_out=./inapi ./api/inapi/*.proto

HTOML_TAG_FIX_CMD = htoml-tag-fix
HTOML_TAG_FIX_ARGS = ./inapi

LYNKAPI_FILTER_CMD = lynkapi-fitter
LYNKAPI_FILTER_V2_ARGS = inapi

.PHONY: api cli server inagent
all: api cli server inagent
	@echo ""
	@echo "build complete"
	@echo ""

api:
	$(PROTOC_CMD) $(PROTOC_V2_ARGS)
	$(HTOML_TAG_FIX_CMD) $(HTOML_TAG_FIX_ARGS)
	$(LYNKAPI_FILTER_CMD) $(LYNKAPI_FILTER_V2_ARGS)

cli:
	go build -o bin/instack cmd/cli/main.go

server:
	go build -o bin/instackd cmd/server/main.go

inagent:
	GOOS=linux GOARCH=amd64 go build -o bin/inagent-linux-amd64 cmd/inagent/inagent.go
	GOOS=linux GOARCH=arm64 go build -o bin/inagent-linux-arm64 cmd/inagent/inagent.go

