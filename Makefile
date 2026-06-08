.PHONY: build test check install-check release-check macos-package macos-check macos-ui-check model-check agent-check agent-check-memory tui-check ui-check ui-check-agent ui-check-full

BREW_LINEA_BIN = $$(brew --prefix bniladridas/linea/linea)/bin/linea

build:
	cd frontend && npm ci && npm run build
	cd backend && go build -o ../bin/linea ./cmd/server

test:
	node scripts/client-api-route-check.mjs
	cd frontend && npm run build
	cd backend && go test ./...
	cd backend && go vet ./...

check: build
	./bin/linea -migrate
	./bin/linea -check

install-check:
	brew tap bniladridas/linea https://github.com/bniladridas/linea
	git -C "$$(brew --prefix)/Library/Taps/bniladridas/homebrew-linea" pull --ff-only
	if brew list --formula bniladridas/linea/linea >/dev/null 2>&1; then \
		brew upgrade bniladridas/linea/linea || test "$$(brew outdated --quiet bniladridas/linea/linea)" = ""; \
	else \
		brew install bniladridas/linea/linea; \
	fi
	brew link --overwrite bniladridas/linea/linea
	brew test bniladridas/linea/linea
	$(BREW_LINEA_BIN) -version

release-check:
	git pull --ff-only
	git -C "$$(brew --repo bniladridas/linea)" pull --ff-only
	brew info bniladridas/linea/linea
	brew upgrade bniladridas/linea/linea
	brew link --overwrite bniladridas/linea/linea
	$(BREW_LINEA_BIN) -version
	$(BREW_LINEA_BIN) -migrate
	$(BREW_LINEA_BIN) -check
	$(MAKE) test

macos-package:
	./scripts/package-macos.sh

macos-check: macos-package
	./scripts/macos-smoke.sh

macos-ui-check: macos-package
	LINEA_MACOS_UI_SMOKE=1 ./scripts/macos-smoke.sh

model-check:
	node scripts/model-smoke.mjs --configured

agent-check: build
	./bin/linea -migrate
	node scripts/agent-smoke.mjs --start

agent-check-memory: build
	LINEA_AGENT_SMOKE_MEMORY=1 node scripts/agent-smoke.mjs --start

tui-check: build
	cd backend && go test ./internal/tui -run TestSmokeCoversPickerNewSearchAndAttachments
	printf ':quit\n' | LINEA_ENV_FILE=/dev/null ./bin/linea -tui
	printf ':quit\n' | LINEA_ENV_FILE=/dev/null ./bin/linea -tui-beta

ui-check:
	node scripts/ui-smoke.mjs --attachment --light-theme --mobile

ui-check-agent:
	node scripts/ui-smoke.mjs --agent-review

ui-check-full:
	node scripts/ui-smoke.mjs --send --search-sources --attachment --light-theme --mobile
