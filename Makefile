.PHONY: all web clean dev test coverage format lint

VERSION ?= $(shell git describe --tags 2>/dev/null || echo 0.1.0)
COMMIT ?= $(shell git describe --match=NeVeRmAtCh --always --abbrev=40 --dirty)
LDFLAGS := -s -w -extldflags "-static" \
	-X github.com/teanode/teanode/internal/version.version=$(VERSION) \
	-X github.com/teanode/teanode/internal/version.commit=$(COMMIT)

all: teanode

web: web/node_modules
	cd web && npm run build

web/node_modules: web/package.json web/package-lock.json
	cd web && npm install
	@touch $@

teanode: web
	CGO_ENABLED=0 go build -ldflags '$(LDFLAGS)' -o teanode .

test:
	go test ./...

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@echo ""
	@go tool cover -func=coverage.out | tail -1

coverage-html: coverage
	go tool cover -html=coverage.out -o coverage.html

format:
	gofmt -w .

lint:
	go vet ./...

clean:
	rm -f teanode
	rm -rf web/node_modules web/dist
	rm -f internal/gateway/static/bundle.* internal/gateway/static/index.html
	rm -f coverage.out coverage.html
