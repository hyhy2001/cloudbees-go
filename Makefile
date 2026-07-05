VERSION := $(shell cat VERSION 2>/dev/null || echo "1.0.0")
BINARY  := dist/bee

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

# Build-time llm-gateway config — read from bee.yaml if present.
# bee.yaml is a flat `key: "value"` file (see bee.yaml.example) — not general YAML.
# LM_URL/LM_API_KEY point at an llm-gateway instance, not a provider
# directly — the real provider credential lives only on the gateway.
BEE_YAML := bee.yaml
yq = $(shell test -f $(BEE_YAML) && sed -n 's/^$(1): *"\(.*\)"$$/\1/p; s/^$(1): *\([^"]*\)$$/\1/p' $(BEE_YAML) | head -1)

LM_URL           ?= $(call yq,proxy_url)
LM_API_KEY       ?= $(call yq,proxy_client_key)
LM_MODEL         ?= $(call yq,model)
LM_REWRITE       ?= $(call yq,rewrite_model)
LM_CLIENT_ID     ?= $(call yq,client_id)
LM_CLIENT_SECRET ?= $(call yq,client_secret)
LM_CHAT_PATH     ?= $(call yq,chat_path)
LM_EMBED_MODEL   ?= $(call yq,embed_model)
LM_EMBED_URL     ?= $(call yq,embed_url)
LM_EMBED_PATH    ?= $(call yq,embed_path)

.PHONY: build test install clean go-install gen-embeddings

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

# bakeconfig XOR-encodes each LM_* value before it ever reaches -ldflags -X,
# so plaintext credentials never land in the linker command line or the binary.
build: go-install
	@$(GO) mod download
	@mkdir -p dist
	@$(GO) build -o "$(GO_DIR)/bakeconfig" ./cmd/bakeconfig
	@LM_URL='$(LM_URL)' LM_API_KEY='$(LM_API_KEY)' LM_MODEL='$(LM_MODEL)' LM_REWRITE='$(LM_REWRITE)' \
		LM_CLIENT_ID='$(LM_CLIENT_ID)' LM_CLIENT_SECRET='$(LM_CLIENT_SECRET)' \
		LM_CHAT_PATH='$(LM_CHAT_PATH)' \
		LM_EMBED_MODEL='$(LM_EMBED_MODEL)' LM_EMBED_URL='$(LM_EMBED_URL)' LM_EMBED_PATH='$(LM_EMBED_PATH)' \
		"$(GO_DIR)/bakeconfig" > "$(GO_DIR)/bakeflags.txt"
	$(GO) build -ldflags="-s -w $$(cat $(GO_DIR)/bakeflags.txt)" -o $(BINARY) ./cmd/bee
	@rm -f "$(GO_DIR)/bakeflags.txt"
	@echo "✓ Built $(BINARY) ($$(du -sh $(BINARY) | cut -f1))"
	@# bee-tui is the interactive TUI in its own binary (see cmd/bee-tui): the
	@# main CLI never links bubbletea, so `bee --ui` execs this alongside it.
	@# No LM_* bakeflags needed — the TUI doesn't use the ask/LM path.
	$(GO) build -ldflags="-s -w" -o dist/bee-tui ./cmd/bee-tui
	@echo "✓ Built dist/bee-tui ($$(du -sh dist/bee-tui | cut -f1))"

test: go-install
	$(GO) test ./...

# Optional: bake neural embeddings for `bee ask` vector search into
# plugins/ask/embeddings_gen.go. Not part of `build` — bee ask works
# BM25-only without this (see plugins/ask/embeddings_gen.go placeholder).
# Requires embed_url/embed_model in bee.yaml (or LM_EMBED_* env).
gen-embeddings: go-install
	LM_URL='$(LM_URL)' LM_API_KEY='$(LM_API_KEY)' LM_CLIENT_ID='$(LM_CLIENT_ID)' LM_CLIENT_SECRET='$(LM_CLIENT_SECRET)' \
		LM_EMBED_MODEL='$(LM_EMBED_MODEL)' LM_EMBED_URL='$(LM_EMBED_URL)' LM_EMBED_PATH='$(LM_EMBED_PATH)' \
		$(GO) run ./cmd/genembeddings

# install = build + delegate to `bee --install` (creates bee.csh + symlink)
install: build
	@$(ROOT_DIR)/$(BINARY) --install
	@echo "✓ Binary ready: $(ROOT_DIR)/$(BINARY)"

clean:
	rm -rf dist/

# Remove Go toolchain and cached modules too
clean-all: clean
	rm -rf "$(GO_DIR)"


