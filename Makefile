VERSION ?= $(shell sh scripts/versions.sh)
DIR_BIN = ./bin
NAME = ferretd
BUF = go run github.com/bufbuild/buf/cmd/buf@v1.72.0
GOLANGCI_LINT_VERSION = v2.13.2
GOLANGCI_LINT_DIR = $(DIR_BIN)/tools/golangci-lint/$(GOLANGCI_LINT_VERSION)
GOLANGCI_LINT_SUFFIX := $(if $(filter windows,$(shell go env GOHOSTOS)),.exe)
GOLANGCI_LINT = $(GOLANGCI_LINT_DIR)/golangci-lint$(GOLANGCI_LINT_SUFFIX)
STDLIB_REF = go run -mod=readonly ./tools/stdlibref
STDLIB_REF_PATH = ./internal/language/stdlib/api.json

.PHONY: install-tools install-lint lint fmt

default: build

build: vet lint test compile

install-tools: install-lint
	go install github.com/bufbuild/buf/cmd/buf@v1.72.0 && \
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12 && \
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2

install-lint: $(GOLANGCI_LINT)

$(GOLANGCI_LINT):
	@set -eu; \
	lint_installer=$$(mktemp); \
	trap 'rm -f "$$lint_installer"' 0; \
	curl --fail --silent --show-error --location \
		"https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_LINT_VERSION)/install.sh" \
		--output "$$lint_installer"; \
	sh "$$lint_installer" -b "$(GOLANGCI_LINT_DIR)" "$(GOLANGCI_LINT_VERSION)"

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

fmt: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) fmt ./...

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) config verify && \
	$(GOLANGCI_LINT) run ./...

vet:
	go vet ./...

release:
	@./scripts/release.sh $(word 2,$(MAKECMDGOALS))

%:
	@:
