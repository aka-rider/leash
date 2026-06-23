.PHONY: build test test-darwin test-integration lint install release-snapshot clean

BINARY := bin/sbx
BUILD_FLAGS := -trimpath -ldflags="-s -w -X main.version=$(shell git describe --tags --always 2>/dev/null || echo dev)"
CGO_ENABLED := 0

build:
	CGO_ENABLED=$(CGO_ENABLED) go build $(BUILD_FLAGS) -o $(BINARY) ./cmd/sbx/

test:
	go test ./pkg/cli/... ./pkg/config/... ./pkg/trace/... ./pkg/detect/...

test-darwin:
	go test -tags darwin -race ./pkg/sandbox/...

test-integration:
	go test -tags 'darwin integration' -race ./pkg/sandbox/... ./pkg/trace/...

lint:
	go vet ./...

install: build
	cp $(BINARY) /usr/local/bin/sbx

release-snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf bin/ dist/
