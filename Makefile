.PHONY: all web clean dev test fmt lint

all: teanode

web: web/node_modules
	cd web && npm run build

web/node_modules: web/package.json web/package-lock.json
	cd web && npm install
	@touch $@

teanode: web
	CGO_ENABLED=0 go build -ldflags '-s -w -extldflags "-static"' -o teanode .

test:
	go test ./...

fmt:
	gofmt -w .

lint:
	go vet ./...

clean:
	rm -f teanode
	rm -rf web/node_modules web/dist
	rm -f internal/gateway/static/bundle.* internal/gateway/static/index.html
