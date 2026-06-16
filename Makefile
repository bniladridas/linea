.PHONY: build build-backend test check run start stop install-check release-check macos-package macos-check macos-ui-check model-check agent-check agent-autonomy-check agent-check-memory tui-check ui-check ui-check-agent ui-check-full android-check

run: build
	@if [ ! -f .env ]; then echo "!! .env not found"; exit 1; fi
	@pkill -f "bin/linea" 2>/dev/null || true
	export $$(grep -v '^#' .env | xargs) && nohup bin/linea > /tmp/linea.log 2>&1 &
	@sleep 2
	@curl -s http://127.0.0.1:8080/healthz && echo ""

start: build-backend
	ANDROID=$(ANDROID) IOS=$(IOS) MACOS=$(MACOS) NODB=$(NODB) ./scripts/start.sh

stop:
	./scripts/stop.sh

BREW_LINEA_BIN = $$(brew --prefix bniladridas/linea/linea)/bin/linea

VERSION ?= $(shell git describe --tags --dirty --always 2>/dev/null || echo dev)

build-backend:
	cd backend && go build -ldflags "-X main.version=$(VERSION)" -o ../bin/linea ./cmd/server

build: build-backend
	cd frontend && npm ci && npm run build

test:
	node scripts/client-api-route-check.mjs
	node scripts/docs-link-check.mjs
	node scripts/tui-command-doc-check.mjs
	cd frontend && npm run build
	cd backend && go test ./...
	cd backend && go vet ./...

check: build
	./bin/linea migrate
	./bin/linea check

install-check:
	brew tap bniladridas/linea https://github.com/bniladridas/linea
	git -C "$$(brew --prefix)/Library/Taps/bniladridas/homebrew-linea" pull --ff-only
	if brew list --formula bniladridas/linea/linea >/dev/null 2>&1; then \
		brew upgrade bniladridas/linea/linea || test "$$(brew outdated --quiet bniladridas/linea/linea)" = ""; \
	else \
		brew install bniladridas/linea/linea; \
	fi
	brew link --overwrite bniladridas/linea/linea
	brew trust bniladridas/linea 2>/dev/null || true
	HOMEBREW_NO_REQUIRE_TAP_TRUST=1 brew test bniladridas/linea/linea
	$(BREW_LINEA_BIN) -version

release-check:
	git pull --ff-only
	git -C "$$(brew --repo bniladridas/linea)" pull --ff-only
	brew info bniladridas/linea/linea
	brew upgrade bniladridas/linea/linea
	brew link --overwrite bniladridas/linea/linea
	$(BREW_LINEA_BIN) -version
	$(BREW_LINEA_BIN) migrate
	$(BREW_LINEA_BIN) check
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
	./bin/linea migrate
	node scripts/agent-smoke.mjs --start

agent-autonomy-check: build
	node scripts/agent-autonomy-smoke.mjs

agent-check-memory: build
	LINEA_AGENT_SMOKE_MEMORY=1 node scripts/agent-smoke.mjs --start

tui-check: build
	cd backend && go test ./internal/tui -run TestSmokeCoversPickerNewSearchAndAttachments
	printf ':quit\n' | LINEA_ENV_FILE=/dev/null ./bin/linea tui
	printf ':quit\n' | LINEA_ENV_FILE=/dev/null ./bin/linea tui-beta

ui-check:
	node scripts/ui-smoke.mjs --attachment --light-theme --mobile

ui-check-agent:
	node scripts/ui-smoke.mjs --agent-review

ui-check-full:
	node scripts/ui-smoke.mjs --send --search-sources --attachment --light-theme --mobile

android-check:
	GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(VERSION)" -o android/app/src/main/assets/linea-android-arm64 ./backend/cmd/server
	cd android && ./gradlew assembleDebug
	@echo "APK at android/app/build/outputs/apk/debug/app-debug.apk"
