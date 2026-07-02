.PHONY: all build run test clean fmt vet

BINARY_NAME=cloakenv
BUILD_DIR=bin

all: build

build:
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/cloakenv

run:
	go run ./cmd/cloakenv

test:
	go test -v ./...

clean:
	rm -rf $(BUILD_DIR)

fmt:
	go fmt ./...

vet:
	go vet ./...
