.PHONY: check release pi-release pi-check agentd-check agentd-release sdk-check studio-check studio-macos-release clean

VERSION := $(shell sed -n '1p' VERSION)
GO_CACHE ?= $(CURDIR)/dist/go-cache
AGENTD_BINARY := $(CURDIR)/dist/hobot-agentd-linux-arm64
SDK_GO_CACHE ?= $(CURDIR)/dist/sdk-go-cache
STUDIO_GO_CACHE ?= $(CURDIR)/dist/studio-go-cache
WAILS_VERSION := v2.12.0

agentd-check:
	cd agentd && GOCACHE="$(GO_CACHE)" go test -race ./...
	cd agentd && GOCACHE="$(GO_CACHE)" go vet ./...

agentd-release: agentd-check
	mkdir -p dist
	cd agentd && GOCACHE="$(GO_CACHE)" CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -trimpath -ldflags '-s -w -X main.version=$(VERSION)' -o "$(AGENTD_BINARY)" .

sdk-check:
	cd sdk/go && GOCACHE="$(SDK_GO_CACHE)" go test -race ./...
	cd sdk/go && GOCACHE="$(SDK_GO_CACHE)" go vet ./...

studio-check: sdk-check
	cd studio/frontend && npm ci --ignore-scripts --no-audit --no-fund
	cd studio/frontend && npm run build
	cd studio && GOCACHE="$(STUDIO_GO_CACHE)" go test -race ./...
	cd studio && GOCACHE="$(STUDIO_GO_CACHE)" go vet ./...

studio-macos-release: studio-check
	cd studio && GOCACHE="$(STUDIO_GO_CACHE)" go run github.com/wailsapp/wails/v2/cmd/wails@$(WAILS_VERSION) build -platform darwin/arm64 -compiler "$(CURDIR)/studio/build/go-macos" -skipbindings -m -clean -nocolour
	./scripts/package-studio-macos.sh

pi-check:
	sh -n scripts/package-pi.sh scripts/install-pi.sh scripts/rollback-pi.sh scripts/hobot-release.sh scripts/uninstall-pi.sh scripts/validate-tar-archive.sh packaging/pi/hobot-launcher
	node -e 'for (const f of process.argv.slice(1)) JSON.parse(require("fs").readFileSync(f, "utf8"))' pi-runtime/package.json packaging/pi/settings.json packaging/pi/models.json packaging/pi/permissions.json packaging/pi/memory.json packaging/pi/goals.json packaging/pi/hooks.json packaging/pi/notifications.json packaging/pi/lsp.json
	node --test tests/*.test.mjs
	node scripts/validate-knowledge.mjs
	node scripts/validate-expert-prompt.mjs
	node scripts/validate-branding.mjs
	node scripts/validate-doc-links.mjs
	node scripts/validate-version.mjs
	node scripts/validate-package.mjs --source .

check: pi-check agentd-check sdk-check

pi-release: pi-check agentd-release
	HOBOT_CODE_AGENTD_BINARY="$(AGENTD_BINARY)" ./scripts/package-pi.sh

release: pi-release

clean:
	rm -rf dist
