.PHONY: all build run test clean fmt vet install uninstall

BINARY_NAME=cloakenv
BUILD_DIR=bin
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

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

install: build
	mkdir -p $(DESTDIR)$(BINDIR)
	install -m 755 $(BUILD_DIR)/$(BINARY_NAME) $(DESTDIR)$(BINDIR)/$(BINARY_NAME)

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/$(BINARY_NAME)

