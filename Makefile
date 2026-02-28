SHELL := /bin/bash

BIN_NAME ?= jan
CMD_PATH := ./cmd/jan
DIST_DIR ?= dist
BIN_PATH := $(DIST_DIR)/$(BIN_NAME)
INSTALL_DIR ?= $(HOME)/.local/bin

VERSION ?= $(shell (git describe --tags --always --dirty 2>/dev/null || git rev-parse --short HEAD 2>/dev/null || echo dev))

.PHONY: help build test install install-system uninstall uninstall-system clean

help:
	@echo "Targets:"
	@echo "  make build           Build binary to $(BIN_PATH)"
	@echo "  make test            Run all tests"
	@echo "  make install         Install to $(INSTALL_DIR)"
	@echo "  make install-system  Install to /usr/local/bin (sudo)"
	@echo "  make uninstall       Remove from $(INSTALL_DIR)"
	@echo "  make uninstall-system Remove from /usr/local/bin (sudo)"
	@echo "  make clean           Remove build artifacts"

build:
	@mkdir -p $(DIST_DIR)
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN_PATH) $(CMD_PATH)

test:
	go test ./...

install: build
	@mkdir -p $(INSTALL_DIR)
	install -m 0755 $(BIN_PATH) $(INSTALL_DIR)/$(BIN_NAME)
	@echo "Installed: $(INSTALL_DIR)/$(BIN_NAME)"

install-system: build
	sudo install -m 0755 $(BIN_PATH) /usr/local/bin/$(BIN_NAME)
	@echo "Installed: /usr/local/bin/$(BIN_NAME)"

uninstall:
	@TARGET="$(INSTALL_DIR)/$(BIN_NAME)"; \
	if [[ -f "$$TARGET" ]]; then \
		rm -f "$$TARGET"; \
		echo "Removed: $$TARGET"; \
	else \
		echo "Not found: $$TARGET"; \
	fi

uninstall-system:
	@TARGET="/usr/local/bin/$(BIN_NAME)"; \
	if sudo test -f "$$TARGET"; then \
		sudo rm -f "$$TARGET"; \
		echo "Removed: $$TARGET"; \
	else \
		echo "Not found: $$TARGET"; \
	fi

clean:
	rm -rf $(DIST_DIR)
