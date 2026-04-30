# =============================================================================
# File: Makefile
# Author: Spicer Matthews <spicer@cloudmanic.com>
# Created: 2026-04-29
# Copyright: 2026 Cloudmanic, LLC. All rights reserved.
# =============================================================================

BINARY := spiceedit

.PHONY: run build install build-linux tidy clean help

# help is the default target so `make` with no args prints what's available.
help:
	@echo "SpiceEdit — opinionated mouse-first terminal code editor"
	@echo ""
	@echo "Targets:"
	@echo "  make run          Run the editor in the current directory."
	@echo "  make build        Build the binary into ./bin/$(BINARY)."
	@echo "  make install      Install ./bin/$(BINARY) into /usr/local/bin."
	@echo "  make build-linux  Cross-compile a static linux/amd64 binary."
	@echo "  make tidy         Run 'go mod tidy'."
	@echo "  make clean        Remove ./bin."

# run starts the editor via 'go run'. Quickest path for development.
# For SSH/production use, prefer 'make build' and ship the binary.
run:
	go run .

# build produces a single binary at ./bin/$(BINARY).
build:
	mkdir -p bin
	go build -o bin/$(BINARY) .

# install copies the binary into /usr/local/bin so you can launch it as `spiceedit`.
install: build
	install -m 0755 bin/$(BINARY) /usr/local/bin/$(BINARY)

# build-linux cross-compiles a fully static linux/amd64 binary. Drop the
# resulting bin/$(BINARY)-linux-amd64 onto a remote box and run it inside
# tmux/zellij — no runtime, no libc, just one file.
build-linux:
	mkdir -p bin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags='-s -w' -o bin/$(BINARY)-linux-amd64 .

# tidy keeps go.mod / go.sum in sync with what's actually imported.
tidy:
	go mod tidy

# clean removes build artifacts.
clean:
	rm -rf bin
