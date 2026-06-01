.PHONY: build test check install-check release-check macos-package model-check ui-check ui-check-full

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
	brew install --HEAD --fetch-HEAD --build-from-source linea
	brew upgrade --fetch-HEAD --build-from-source linea
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

ui-check:
	node scripts/ui-smoke.mjs

ui-check-full:
	node scripts/ui-smoke.mjs --send
