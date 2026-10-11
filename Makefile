.PHONY: frontend backend build run check

frontend:
	cd frontend && pnpm install --frozen-lockfile && pnpm build

backend: frontend
	go build -o openslot ./cmd/calnode

build: backend

run:
	./scripts/run-local.sh

check:
	cd frontend && pnpm check && pnpm test:visual
	go test -timeout 5m ./...
	node --test internal/handler/assets/*.test.js
	cd frontend && pnpm test:e2e
