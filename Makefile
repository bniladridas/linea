.PHONY: build test check install-check release-check macos-package model-check agent-check agent-check-memory ui-check ui-check-full

build:
	cd frontend && npm ci && npm run build
	cd backend && go build -o ../bin/linea ./cmd/server

test:
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
		brew test bniladridas/linea/linea; \
	else \
		brew install bniladridas/linea/linea; \
	fi
	linea -version

release-check:
	git pull --ff-only
	git -C "$$(brew --repo bniladridas/linea)" pull --ff-only
	brew info bniladridas/linea/linea
	brew upgrade bniladridas/linea/linea
	linea -version
	linea -migrate
	linea -check
	$(MAKE) test

macos-package:
	./scripts/package-macos.sh

model-check:
	node scripts/model-smoke.mjs --configured

agent-check: build
	./bin/linea -migrate
	node scripts/agent-smoke.mjs --start

agent-check-memory: build
	LINEA_AGENT_SMOKE_MEMORY=1 node scripts/agent-smoke.mjs --start

ui-check:
	node scripts/ui-smoke.mjs --attachment --light-theme --mobile

ui-check-full:
	node scripts/ui-smoke.mjs --send --search-sources --attachment --light-theme --mobile
