APP := aster
VERSION ?= 0.6.0
LEGACY_VERSION ?= 0.4.0
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf none)
LDFLAGS := -s -w -X github.com/Kyrie-w8/aster-edge/internal/cli.Version=$(VERSION) -X github.com/Kyrie-w8/aster-edge/internal/cli.Commit=$(COMMIT)
GOCACHE ?= /tmp/aster-go-cache
GOMODCACHE ?= /tmp/aster-go-modcache

.PHONY: build test check linux-arm64 legacy-release release pi-release pi-check clean

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

legacy-release:
	$(MAKE) linux-arm64 VERSION=$(LEGACY_VERSION)
	./scripts/package.sh $(LEGACY_VERSION)

pi-check:
	sh -n scripts/package-pi.sh scripts/install-pi.sh scripts/rollback-pi.sh packaging/pi/aster-launcher
	node -e 'for (const f of process.argv.slice(1)) JSON.parse(require("fs").readFileSync(f, "utf8"))' pi-runtime/package.json packaging/pi/settings.json packaging/pi/models.json
	node scripts/validate-knowledge.mjs

pi-release: pi-check
	./scripts/package-pi.sh $(VERSION)

release: pi-release

clean:
	rm -rf dist
