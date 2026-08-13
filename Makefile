.PHONY: dev build build-web test lint

dev:
	go run ./cmd/tako serve --dev

build-web:
	cd web && bun run build

build: build-web
	mkdir -p bin
	$(RM) bin/tako-sessiond bin/tako-bridge
	GOCACHE=/tmp/tako-go-cache go build -buildvcs=false -trimpath -ldflags="-s -w" -o bin/tako ./cmd/tako

test:
	GOCACHE=/tmp/tako-go-cache go test ./...
	cd web && bun run typecheck

lint:
	GOCACHE=/tmp/tako-go-cache go vet ./...
	cd web && bun run lint
