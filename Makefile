.PHONY: build test test-darwin test-integration lint install release-snapshot clean

BUILD_FLAGS := -trimpath -ldflags="-s -w -X main.version=$(shell git describe --tags --always 2>/dev/null || echo dev)"
CGO_ENABLED := 0

build:
	CGO_ENABLED=$(CGO_ENABLED) go build $(BUILD_FLAGS) -o bin/leash ./cmd/leash/
	CGO_ENABLED=$(CGO_ENABLED) go build $(BUILD_FLAGS) -o bin/leash-trace ./cmd/trace/

test:
	go test ./internal/cli/... ./config/... ./detect/... .

test-darwin:
	go test -tags darwin -race ./sandbox/... .

test-integration:
	go test -tags 'darwin integration' -race ./sandbox/... ./cmd/trace/...

lint:
	go vet ./...

install: build
	cp bin/leash /usr/local/bin/leash
	cp bin/leash-trace /usr/local/bin/leash-trace

release-snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf bin/ dist/
