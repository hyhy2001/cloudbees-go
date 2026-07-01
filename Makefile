VERSION := $(shell cat VERSION 2>/dev/null || echo "1.0.0")
BINARY  := dist/bee
MODULE  := bee

# All tools (Go, deps) live inside this directory — nothing touches system or ~/.local
ROOT_DIR  := $(shell pwd)
GO_DIR    := $(ROOT_DIR)/.go
GOROOT    := $(GO_DIR)/go
GOPATH    := $(GO_DIR)/gopath
GOBIN     := $(GOROOT)/bin/go
GO        := PATH="$(GOROOT)/bin:$$PATH" GOROOT="$(GOROOT)" GOPATH="$(GOPATH)" go

GO_VERSION     := 1.22.2
GO_TARBALL     := go$(GO_VERSION).linux-amd64.tar.gz
GO_DOWNLOAD_URL := https://go.dev/dl/$(GO_TARBALL)

# Build-time LM credentials — read from bee.lm.json if present
-include .bee-build.mk

LM_URL     ?=
LM_API_KEY ?=
LM_MODEL   ?=
LM_REWRITE ?=
LM_CLIENT_ID     ?=
LM_CLIENT_SECRET ?=
LM_CHAT_PATH     ?=
LM_EMBED_MODEL   ?=
LM_EMBED_URL     ?=
LM_EMBED_PATH    ?=

LDFLAGS := -s -w \
  -X '$(MODULE)/internal/config.BakedLMURL=$(LM_URL)' \
  -X '$(MODULE)/internal/config.BakedAPIKey=$(LM_API_KEY)' \
  -X '$(MODULE)/internal/config.BakedModel=$(LM_MODEL)' \
  -X '$(MODULE)/internal/config.BakedRewriteModel=$(LM_REWRITE)' \
  -X '$(MODULE)/internal/config.BakedClientID=$(LM_CLIENT_ID)' \
  -X '$(MODULE)/internal/config.BakedClientSecret=$(LM_CLIENT_SECRET)' \
  -X '$(MODULE)/internal/config.BakedChatPath=$(LM_CHAT_PATH)' \
  -X '$(MODULE)/internal/config.BakedEmbedModel=$(LM_EMBED_MODEL)' \
  -X '$(MODULE)/internal/config.BakedEmbedURL=$(LM_EMBED_URL)' \
  -X '$(MODULE)/internal/config.BakedEmbedPath=$(LM_EMBED_PATH)'

.PHONY: build test install clean go-install

# Download and extract Go into .go/go/ inside this project directory
go-install:
	@if [ ! -x "$(GOBIN)" ]; then \
		echo "Go not found — downloading $(GO_VERSION) into $(GO_DIR)..."; \
		mkdir -p "$(GO_DIR)"; \
		curl -fsSL "$(GO_DOWNLOAD_URL)" -o "$(GO_DIR)/$(GO_TARBALL)"; \
		tar -C "$(GO_DIR)" -xzf "$(GO_DIR)/$(GO_TARBALL)"; \
		rm "$(GO_DIR)/$(GO_TARBALL)"; \
		echo "✓ Go $(GO_VERSION) installed at $(GOROOT)"; \
	else \
		echo "Go $$($(GOBIN) version | awk '{print $$3}') found at $(GOROOT)"; \
	fi

build: go-install
	@$(GO) mod download
	@mkdir -p dist
	$(GO) build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/bee
	@echo "✓ Built $(BINARY) ($$(du -sh $(BINARY) | cut -f1))"

test: go-install
	$(GO) test ./...

# install = just build; binary lives at dist/bee inside this directory
install: build
	@echo "✓ Binary ready: $(ROOT_DIR)/$(BINARY)"

clean:
	rm -rf dist/

# Remove Go toolchain and cached modules too
clean-all: clean
	rm -rf "$(GO_DIR)"


