.PHONY: build test check install-check model-check ui-check ui-check-full

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
	brew install --HEAD --fetch-HEAD --build-from-source linea
	linea -version

model-check:
	node scripts/model-smoke.mjs --configured

ui-check:
	node scripts/ui-smoke.mjs

ui-check-full:
	node scripts/ui-smoke.mjs --send
