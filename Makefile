VERSION ?= $(shell sh scripts/versions.sh)
DIR_BIN = ./bin
NAME = ferretd
BUF = go run github.com/bufbuild/buf/cmd/buf@v1.72.0
STDLIB_REF = go run -mod=readonly ./tools/stdlibref
STDLIB_REF_PATH = ./internal/language/stdlib/api.json
CLIENT_DIR = ./client
CMD_DIR = ./cmd
INTERNAL_DIR = ./internal

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

generate:
	$(STDLIB_REF) sync -output $(STDLIB_REF_PATH)
	$(BUF) generate

proto-lint:
	$(BUF) lint

check-generate:
	$(STDLIB_REF) check -output $(STDLIB_REF_PATH)
	$(BUF) generate
	git diff --exit-code -- gen $(STDLIB_REF_PATH) && \
	test -z "$$(git status --porcelain --untracked-files=all -- gen $(STDLIB_REF_PATH))"

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
