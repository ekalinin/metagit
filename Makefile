GO ?= go
NPM ?= npm
BIN := bin/metagit

.PHONY: all build web go test test-go test-web lint clean

all: build

build: web go

web: web/node_modules
	cd web && $(NPM) run build

web/node_modules: web/package.json web/package-lock.json
	cd web && $(NPM) ci
	@touch $@

go:
	CGO_ENABLED=0 $(GO) build -o $(BIN) ./cmd/metagit

test: test-go test-web

test-go:
	$(GO) test ./...

test-web: web/node_modules
	cd web && $(NPM) test

lint: web/node_modules
	@unformatted="$$(gofmt -l cmd internal examples)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	$(GO) vet ./...
	cd web && $(NPM) run typecheck

clean:
	rm -rf bin internal/webui/dist/app
