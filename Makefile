.DEFAULT_GOAL := all

.PHONY: all help generate teanode web web-deps test coverage coverage-html format lint clean \
	test-voice-e2e test-voice-e2e-smoke test-voice-e2e-compare test-voice-e2e-scenario

GO ?= go
NPM ?= npm
WEB_DIR ?= web
BINARY ?= teanode

VOICE_E2E_CMD ?= ./test/voicee2e/cmd/voicee2e
VOICE_E2E_SUITE ?= test/voicee2e/scenarios/suite.yaml
VOICE_E2E_REPORT_DIR ?= test/voicee2e/reports
VOICE_E2E_SCENARIO ?=
VOICE_E2E_OUT ?= $(VOICE_E2E_REPORT_DIR)/scenario.json

PROMPT_A ?=
PROMPT_B ?=

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

web: web-deps ## Build frontend bundle
	cd $(WEB_DIR) && $(NPM) run build

web-deps: $(WEB_DIR)/node_modules ## Install frontend dependencies

$(WEB_DIR)/node_modules: $(WEB_DIR)/package.json $(WEB_DIR)/package-lock.json
	cd $(WEB_DIR) && $(NPM) install
	@touch $@

teanode: generate ## Build teanode binary
	CGO_ENABLED=0 $(GO) build -ldflags '$(LDFLAGS)' -o $(BINARY) .

test: ## Run backend tests
	$(GO) test ./...

coverage: ## Run backend tests with coverage report
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out
	@echo ""
	@$(GO) tool cover -func=coverage.out | tail -1

coverage-html: coverage ## Generate HTML coverage report
	$(GO) tool cover -html=coverage.out -o coverage.html

format: ## Format Go code
	gofmt -w .

lint: ## Run backend lint checks
	$(GO) vet ./...

clean: ## Remove build and coverage artifacts
	rm -f $(BINARY)
	rm -rf $(WEB_DIR)/node_modules $(WEB_DIR)/dist
	rm -f internal/gateway/static/bundle.* internal/gateway/static/index.html
	rm -f coverage.out coverage.html

test-voice-e2e: ## Run full voice e2e suite
	$(GO) run $(VOICE_E2E_CMD) --suite $(VOICE_E2E_SUITE) --out $(VOICE_E2E_REPORT_DIR)/full.json

test-voice-e2e-smoke: ## Run smoke voice e2e scenarios
	$(GO) run $(VOICE_E2E_CMD) --suite $(VOICE_E2E_SUITE) --scenario s1_short --out $(VOICE_E2E_REPORT_DIR)/smoke.json
	$(GO) run $(VOICE_E2E_CMD) --suite $(VOICE_E2E_SUITE) --scenario s5_barge_in --out $(VOICE_E2E_REPORT_DIR)/smoke-barge.json

test-voice-e2e-scenario: ## Run a single voice e2e scenario (SCENARIO=<id> OUT=<path>)
	@test -n "$(VOICE_E2E_SCENARIO)" || (echo "VOICE_E2E_SCENARIO is required"; exit 1)
	$(GO) run $(VOICE_E2E_CMD) --suite $(VOICE_E2E_SUITE) --scenario $(VOICE_E2E_SCENARIO) --out $(VOICE_E2E_OUT)

test-voice-e2e-compare: ## Compare prompt files (PROMPT_A=<file> PROMPT_B=<file>)
	@test -n "$(PROMPT_A)" || (echo "PROMPT_A is required"; exit 1)
	@test -n "$(PROMPT_B)" || (echo "PROMPT_B is required"; exit 1)
	$(GO) run $(VOICE_E2E_CMD) --suite $(VOICE_E2E_SUITE) --prompt $(PROMPT_A) --out $(VOICE_E2E_REPORT_DIR)/prompt-a.json
	$(GO) run $(VOICE_E2E_CMD) --suite $(VOICE_E2E_SUITE) --prompt $(PROMPT_B) --out $(VOICE_E2E_REPORT_DIR)/prompt-b.json
	$(GO) run $(VOICE_E2E_CMD) --compare --prompt-a $(VOICE_E2E_REPORT_DIR)/prompt-a.json --prompt-b $(VOICE_E2E_REPORT_DIR)/prompt-b.json --out $(VOICE_E2E_REPORT_DIR)/prompt-compare.json
