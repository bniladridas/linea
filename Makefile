.PHONY: build test check model-check ui-check ui-check-full

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

model-check:
	node scripts/model-smoke.mjs --configured

ui-check:
	node scripts/ui-smoke.mjs

ui-check-full:
	node scripts/ui-smoke.mjs --send
