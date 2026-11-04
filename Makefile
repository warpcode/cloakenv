.PHONY: all build run test bench test-all clean fmt vet install uninstall verify agents-check

BINARY_NAME=cloakenv
BUILD_DIR=bin

all: build

build:
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) .

run:
	go run .

test:
	go test -v -race ./...

bench:
	go test -bench=. ./internal/engine/...

test-all: fmt vet test bench

clean:
	rm -rf $(BUILD_DIR)

fmt:
	go fmt ./...

vet:
	go vet ./...

install:
	go install

uninstall:
	go clean -i

verify:
	@echo "==> Running full verification pipeline..."
	go build ./...
	go vet ./...
	@test -z "$$(gofmt -l .)" || (echo "Unformatted Go files detected" && gofmt -l . && exit 1)
	go test -race ./...
	@echo "==> Verification passed."

agents-check:
	@./scripts/check_agents.sh

