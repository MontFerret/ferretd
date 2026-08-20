VERSION ?= $(shell sh scripts/versions.sh)
DIR_BIN = ./bin
NAME = ferretd
BUF = go run github.com/bufbuild/buf/cmd/buf@v1.72.0
CLIENT_DIR = ./client
CMD_DIR = ./cmd
INTERNAL_DIR = ./internal
STDLIB_REFERENCE = ./internal/language/stdlib/api.json

.PHONY: build check-generate compile default fmt generate generate-stdlib install install-tools lint proto-lint test vet

default: build

build: vet lint test compile

install-tools:
	go install honnef.co/go/tools/cmd/staticcheck@latest && \
	go install golang.org/x/tools/cmd/goimports@latest && \
	go install github.com/mgechev/revive@latest && \
	go install github.com/bufbuild/buf/cmd/buf@v1.72.0 && \
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12 && \
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2

install:
	go mod download

compile:
	go build -v -o ${DIR_BIN}/${NAME} \
	-ldflags "-X main.version=${VERSION}" \
	./cmd/ferretd

test:
	go test ./...

generate: generate-stdlib
	$(BUF) generate

generate-stdlib:
	go run -mod=readonly ./tools/stdlibref -output ${STDLIB_REFERENCE}

proto-lint:
	$(BUF) lint

check-generate:
	go run -mod=readonly ./tools/stdlibref -check -output ${STDLIB_REFERENCE}
	$(MAKE) generate
	git diff --exit-code -- gen ${STDLIB_REFERENCE} && \
	test -z "$$(git status --porcelain --untracked-files=all -- gen ${STDLIB_REFERENCE})"

fmt:
	go fmt ./... && \
	goimports -w -local github.com/MontFerret ${INTERNAL_DIR} ${CMD_DIR} ${CLIENT_DIR}

lint:
	staticcheck ./... && \
	revive -config revive.toml -formatter stylish -exclude ./vendor/... ./...


vet:
	go vet ./...

release:
	@./scripts/release.sh $(word 2,$(MAKECMDGOALS))

%:
	@:
