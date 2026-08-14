.PHONY: check release pi-release pi-check agentd-check agentd-release sdk-check examples-check studio-check studio-macos-release model-egress-board-check install-lifecycle-board-check board-acceptance-check release-candidate-check clean

VERSION := $(shell sed -n '1p' VERSION)
GO_CACHE ?= $(CURDIR)/dist/go-cache
AGENTD_BINARY := $(CURDIR)/dist/hobot-agentd-linux-arm64
SDK_GO_CACHE ?= $(CURDIR)/dist/sdk-go-cache
STUDIO_GO_CACHE ?= $(CURDIR)/dist/studio-go-cache
WAILS_VERSION := v2.12.0

agentd-check:
	cd agentd && GOCACHE="$(GO_CACHE)" go test -race ./...
	cd agentd && GOCACHE="$(GO_CACHE)" go vet ./...
	cd agentd && GOCACHE="$(GO_CACHE)" CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -trimpath -o /dev/null .

agentd-release: agentd-check
	mkdir -p dist
	cd agentd && GOCACHE="$(GO_CACHE)" CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -trimpath -ldflags '-s -w -X main.version=$(VERSION) -X main.releaseMarker=HOBOT_CODE_AGENTD_VERSION=$(VERSION);' -o "$(AGENTD_BINARY)" .

sdk-check:
	cd sdk/go && GOCACHE="$(SDK_GO_CACHE)" go test -race ./...
	cd sdk/go && GOCACHE="$(SDK_GO_CACHE)" go vet ./...

examples-check:
	sh -n examples/regnet-x5/convert_x5.sh
	node -e 'JSON.parse(require("fs").readFileSync("examples/regnet-x5/source-lock.json", "utf8"))'
	PYTHONPYCACHEPREFIX="$(CURDIR)/dist/pycache" python3 -m unittest examples/regnet-x5/test_validate_x5.py
	PYTHONPYCACHEPREFIX="$(CURDIR)/dist/pycache" python3 -m compileall -q examples

studio-check: sdk-check
	cd studio/frontend && npm ci --ignore-scripts --no-audit --no-fund
	cd studio/frontend && npm test
	cd studio/frontend && npm run build
	cd studio && GOCACHE="$(STUDIO_GO_CACHE)" go test -race ./...
	cd studio && GOCACHE="$(STUDIO_GO_CACHE)" go vet ./...

studio-macos-release: studio-check
	cd studio && GOCACHE="$(STUDIO_GO_CACHE)" go run github.com/wailsapp/wails/v2/cmd/wails@$(WAILS_VERSION) build -platform darwin/arm64 -compiler "$(CURDIR)/studio/build/go-macos" -skipbindings -m -clean -nocolour
	./scripts/package-studio-macos.sh

pi-check:
	sh -n scripts/package-pi.sh scripts/install-pi.sh scripts/rollback-pi.sh scripts/hobot-release.sh scripts/uninstall-pi.sh scripts/validate-tar-archive.sh packaging/pi/hobot-launcher
	PYTHONPYCACHEPREFIX="$(CURDIR)/dist/pycache" python3 -m py_compile scripts/verify-model-egress-runtime.py scripts/verify-install-lifecycle.py
	PYTHONPYCACHEPREFIX="$(CURDIR)/dist/pycache" python3 -m unittest discover -s tests -p 'test_verify_*.py'
	node -e 'for (const f of process.argv.slice(1)) JSON.parse(require("fs").readFileSync(f, "utf8"))' pi-runtime/package.json extensions/catalog.json packaging/pi/settings.json packaging/pi/models.json packaging/pi/providers.json packaging/pi/permissions.json packaging/pi/memory.json packaging/pi/goals.json packaging/pi/hooks.json packaging/pi/notifications.json packaging/pi/lsp.json
	node --test tests/*.test.mjs
	node scripts/validate-knowledge.mjs
	node scripts/validate-expert-prompt.mjs
	node scripts/validate-branding.mjs
	node scripts/validate-doc-links.mjs
	node scripts/validate-version.mjs
	node scripts/validate-pi-compatibility.mjs --source .
	node scripts/validate-package.mjs --source .

check: pi-check agentd-check sdk-check examples-check

pi-release: pi-check agentd-release
	HOBOT_CODE_AGENTD_BINARY="$(AGENTD_BINARY)" ./scripts/package-pi.sh

model-egress-board-check:
	@test -n "$(PACKAGE_ROOT)" || (echo 'PACKAGE_ROOT must point to an extracted Linux ARM64 release package' >&2; exit 2)
	python3 scripts/verify-model-egress-runtime.py --package-root "$(PACKAGE_ROOT)" $(if $(REPORT),--output "$(REPORT)") $(if $(RPC_REPORT),--rpc-output "$(RPC_REPORT)") $(if $(SESSION_REPORT),--session-output "$(SESSION_REPORT)") $(if $(EXTENSION_REPORT),--extension-output "$(EXTENSION_REPORT)") $(if $(TUI_REPORT),--tui-output "$(TUI_REPORT)") $(if $(READINESS_REPORT),--readiness-output "$(READINESS_REPORT)")

install-lifecycle-board-check:
	@test -n "$(PACKAGE_ROOT)" || (echo 'PACKAGE_ROOT must point to an extracted Linux ARM64 release package' >&2; exit 2)
	@test -n "$(REPORT)" || (echo 'REPORT must be a private report path outside the package root' >&2; exit 2)
	python3 scripts/verify-install-lifecycle.py --package-root "$(PACKAGE_ROOT)" --output "$(REPORT)" $(if $(USER),--user "$(USER)")

board-acceptance-check:
	@test -n "$(REPORT_DIR)" || (echo 'REPORT_DIR must contain only private acceptance reports for one candidate build' >&2; exit 2)
	node scripts/validate-board-acceptance.mjs --expected-version "$(VERSION)" --reports "$(REPORT_DIR)" $(if $(SCENARIO),--scenario "$(SCENARIO)") $(if $(REPORT),--output "$(REPORT)")

release-candidate-check:
	@test -n "$(PACKAGE_ROOT)" || (echo 'PACKAGE_ROOT must point to the extracted Linux ARM64 candidate' >&2; exit 2)
	@test -n "$(ARCHIVE)" || (echo 'ARCHIVE must point to the exact Linux ARM64 candidate archive' >&2; exit 2)
	@test -n "$(MATRIX)" || (echo 'MATRIX must point to the sanitized full board acceptance matrix' >&2; exit 2)
	@test -n "$(EXPECTED_COMMIT)" || (echo 'EXPECTED_COMMIT must be the exact 40-character release tag commit' >&2; exit 2)
	@test -n "$(EVIDENCE)" || (echo 'EVIDENCE must be a new output path for public release evidence' >&2; exit 2)
	node scripts/validate-release-candidate.mjs --package-root "$(PACKAGE_ROOT)" --archive "$(ARCHIVE)" --matrix "$(MATRIX)" --expected-version "$(VERSION)" --expected-commit "$(EXPECTED_COMMIT)" --output "$(EVIDENCE)"

release: pi-release

clean:
	rm -rf dist
