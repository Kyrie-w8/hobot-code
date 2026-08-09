VERSION ?= 0.9.0

.PHONY: check release pi-release pi-check clean

pi-check:
	sh -n scripts/package-pi.sh scripts/install-pi.sh scripts/rollback-pi.sh packaging/pi/hobot-launcher
	node -e 'for (const f of process.argv.slice(1)) JSON.parse(require("fs").readFileSync(f, "utf8"))' pi-runtime/package.json packaging/pi/settings.json packaging/pi/models.json packaging/pi/permissions.json
	node --test tests/*.test.mjs
	node scripts/validate-knowledge.mjs
	node scripts/validate-expert-prompt.mjs
	node scripts/validate-branding.mjs

check: pi-check

pi-release: pi-check
	./scripts/package-pi.sh $(VERSION)

release: pi-release

clean:
	rm -rf dist
