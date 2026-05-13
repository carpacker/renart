GO ?= /usr/local/go/bin/go
PNPM ?= corepack pnpm
DOCKER ?= docker
DOCS_IMAGE ?= renart-docs:local
RENART_VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo local)
RENART_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

.PHONY: help build test check go-build go-test web-install web-build web-typecheck web-test-live docs-install docs-build docs-dev docs-preview docs-screenshots landing-media docs-docker docs-docker-run sync-install clean

help:
	@printf "Renart build targets\n\n"
	@printf "  make build             Build Go binary, web app, and docs\n"
	@printf "  make check             Run Go tests plus web/docs builds\n"
	@printf "  make go-build          Build Renart CLI\n"
	@printf "  make go-test           Run Go tests\n"
	@printf "  make web-build         Build React app\n"
	@printf "  make web-typecheck     Typecheck React app\n"
	@printf "  make web-test-live     Run live Playwright tests\n"
	@printf "  make docs-build        Build Astro/Starlight docs\n"
	@printf "  make docs-dev          Start docs dev server\n"
	@printf "  make docs-screenshots  Regenerate docs quickstart screenshots\n"
	@printf "  make landing-media     Regenerate landing media\n"
	@printf "  make docs-docker       Build Caddy docs image\n"
	@printf "  make docs-docker-run   Serve docs image on http://127.0.0.1:8099\n"

build: go-build web-build docs-build

check: go-test web-build docs-build

test: go-test

go-build:
	$(GO) build .

go-test:
	$(GO) test ./...

web-install:
	$(PNPM) --dir web install --frozen-lockfile

web-build:
	$(PNPM) --dir web build

web-typecheck:
	$(PNPM) --dir web typecheck

web-test-live:
	$(PNPM) --dir web test:e2e:live

docs-install:
	$(PNPM) --dir docs install --frozen-lockfile

docs-build:
	$(PNPM) --dir docs build

docs-dev:
	$(PNPM) --dir docs dev

docs-preview:
	$(PNPM) --dir docs preview

docs-screenshots:
	$(PNPM) --dir web docs:screenshots

landing-media:
	$(PNPM) --dir web landing:media

sync-install:
	cp install.sh docs/public/install.sh

docs-docker:
	$(DOCKER) build -f Dockerfile.docs --build-arg RENART_VERSION=$(RENART_VERSION) --build-arg RENART_COMMIT=$(RENART_COMMIT) -t $(DOCS_IMAGE) .

docs-docker-run:
	$(DOCKER) run --rm -p 127.0.0.1:8099:80 $(DOCS_IMAGE)

clean:
	rm -rf dist web/dist docs/dist docs/.astro
