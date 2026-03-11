.DEFAULT_GOAL := all

.PHONY: all help generate teanode web test coverage format lint clean

GO ?= go
NPM ?= npm
WEB_DIR ?= web
BINARY ?= teanode
GOLANGCI_LINT ?= golangci-lint

VERSION ?= $(shell git describe --tags 2>/dev/null || echo 0.1.0)
COMMIT ?= $(shell git describe --match=NeVeRmAtCh --always --abbrev=40 --dirty)
LDFLAGS := -s -w -extldflags "-static" \
	-X github.com/teanode/teanode/internal/version.version=$(VERSION) \
	-X github.com/teanode/teanode/internal/version.commit=$(COMMIT)

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "; print "Targets:"} /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

all: web teanode ## Build backend binary and frontend bundle

generate: ## Run code generators
	$(GO) generate ./...

web: $(WEB_DIR)/node_modules ## Build frontend bundle
	cd $(WEB_DIR) && $(NPM) run build

$(WEB_DIR)/node_modules: $(WEB_DIR)/package.json $(WEB_DIR)/package-lock.json
	cd $(WEB_DIR) && $(NPM) install
	@touch $@

teanode: generate ## Build teanode binary
	CGO_ENABLED=0 $(GO) build -ldflags '$(LDFLAGS)' -o $(BINARY) .

test: ## Run backend tests with coverage
	gotestsum --format pkgname -- -coverprofile=coverage.out ./...
	@echo ""
	@$(GO) tool cover -func=coverage.out | tail -1

coverage: test ## Generate HTML coverage report
	$(GO) tool cover -html=coverage.out -o coverage.html

format: ## Format Go code
	find . -name '*.go' -not -path './vendor/*' | xargs gofmt -l -w

lint: ## Run backend lint checks
	$(GOLANGCI_LINT) run ./...

clean: ## Remove build and coverage artifacts
	rm -f $(BINARY)
	rm -rf $(WEB_DIR)/node_modules $(WEB_DIR)/dist
	rm -f internal/gateway/static/bundle.* internal/gateway/static/index.html
	rm -f coverage.out coverage.html junit.xml

