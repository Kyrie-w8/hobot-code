APP := aster
VERSION ?= 0.2.0
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf none)
LDFLAGS := -s -w -X github.com/Kyrie-w8/aster-edge/internal/cli.Version=$(VERSION) -X github.com/Kyrie-w8/aster-edge/internal/cli.Commit=$(COMMIT)
GOCACHE ?= /tmp/aster-go-cache
GOMODCACHE ?= /tmp/aster-go-modcache

.PHONY: build test check linux-arm64 release clean

build:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(APP) ./cmd/aster

test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...

check:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go vet ./...
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test -race ./...

linux-arm64:
	mkdir -p dist
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(APP)-linux-arm64 ./cmd/aster

release: linux-arm64
	./scripts/package.sh $(VERSION)

clean:
	rm -rf dist
