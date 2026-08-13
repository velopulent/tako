# Repository Guidelines

## Project Structure & Module Organization

Tako is a Go modular monolith with an embedded React dashboard. `cmd/tako/` contains the single executable entry point and its `serve`, `sessiond`, and `bridge` modes. Backend packages live under `internal/`; keep platform-specific logic in small module-owned adapters rather than generic repository layers. The API contract is in `api/openapi.yaml`.

Frontend source lives in `web/src/`. Reusable shadcn components belong in `web/src/components/ui/`, application components in `web/src/components/`, and route views in `web/src/routes/`. Vite writes production assets to `internal/webui/dist/` for `go:embed`. Architecture decisions and terminology are documented in `docs/`; deployment files are in `packaging/`.

## Build, Test, and Development Commands

- `cd web && bun install`: install frontend dependencies from `bun.lock`.
- `make dev`: run the loopback HTTP development gateway on port 9090.
- `make build-web`: type-check and build the embedded SPA.
- `make build`: produce the single stripped `bin/tako` multicall executable.
- `make test`: run Go tests and frontend type checking.
- `make lint`: run `go vet` and ESLint.
- `GOCACHE=/tmp/tako-go-cache go test -race ./...`: run the full Go race suite before security-sensitive changes.

## Coding Style & Naming Conventions

Format Go with `gofmt`; use short lowercase package names and exported PascalCase identifiers. Keep HTTP handlers and platform adapters focused and context-aware. Go tests use `*_test.go` and `TestBehavior` names.

Use TypeScript, two-space indentation, PascalCase React components, and `kebab-case.tsx` filenames. Run `bun run format`, `bun run typecheck`, and `bun run lint` from `web/`. Prefer existing shadcn/Base UI components over bespoke controls.

## Testing Guidelines

Place tests beside implementation. Cover parsers, bounded streams, session expiry, authorization boundaries, bridge framing, and cancellation. API changes should include handler tests and update `api/openapi.yaml`. UI changes should exercise loading, error, empty, keyboard, mobile, and dark-mode states.

## Commit & Pull Request Guidelines

Git history is unavailable in this checkout. Use concise imperative subjects, optionally Conventional Commit prefixes such as `feat:`, `fix:`, or `docs:`. Keep commits scoped. Pull requests should explain behavior and security impact, list verification commands, link relevant issues/ADRs, and include screenshots for visible UI changes.

## Security & Configuration

Never move privileged work into the network-facing gateway. Preserve separate process modes and systemd sandboxes. Do not store passwords, session tokens, certificates, or local configuration in Git; start from `config.example.toml`.
