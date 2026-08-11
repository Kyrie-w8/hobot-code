.PHONY: check release pi-release pi-check agentd-check agentd-release clean

VERSION := $(shell sed -n '1p' VERSION)
GO_CACHE ?= $(CURDIR)/dist/go-cache
AGENTD_BINARY := $(CURDIR)/dist/hobot-agentd-linux-arm64

agentd-check:
	cd agentd && GOCACHE="$(GO_CACHE)" go test -race ./...
	cd agentd && GOCACHE="$(GO_CACHE)" go vet ./...

agentd-release: agentd-check
	mkdir -p dist
	cd agentd && GOCACHE="$(GO_CACHE)" CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -trimpath -ldflags '-s -w -X main.version=$(VERSION)' -o "$(AGENTD_BINARY)" .

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

check: pi-check agentd-check

pi-release: pi-check agentd-release
	HOBOT_CODE_AGENTD_BINARY="$(AGENTD_BINARY)" ./scripts/package-pi.sh

release: pi-release

clean:
	rm -rf dist
