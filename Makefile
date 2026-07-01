VERSION := $(shell cat VERSION 2>/dev/null || echo "1.0.0")
BINARY  := dist/bee
MODULE  := github.com/hyhy2001/bee

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

.PHONY: build test install clean

build:
	@mkdir -p dist
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/bee
	@echo "✓ Built $(BINARY) ($$(du -sh $(BINARY) | cut -f1))"

test:
	go test ./...

install: build
	@mkdir -p ~/.local/bin
	cp $(BINARY) ~/.local/bin/bee
	@echo "✓ Installed to ~/.local/bin/bee"

clean:
	rm -rf dist/
