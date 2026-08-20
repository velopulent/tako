# Repository Guidelines

## Project Structure & Module Organization

Tako is a linux administration application

This codebase is an Nx monorepo managed with Bun. Applications live under `apps/`. `apps/backend/cmd/tako/` contains the single Go executable entry point and its `serve`, `sessiond`, and `bridge` modes. Backend packages live under `apps/backend/internal/`; keep platform-specific logic in small module-owned adapters rather than generic repository layers. The API contract is in `apps/backend/api/openapi.yaml`.

Frontend source lives in `apps/dashboard/src/`. Reusable shadcn components belong in `apps/dashboard/src/components/ui/`, application components in `apps/dashboard/src/components/`, and route views in `apps/dashboard/src/routes/`. Vite writes production assets to `apps/backend/internal/dashboard/dist/` for `go:embed`. Architecture decisions and terminology are documented in `docs/`; deployment files are in `apps/backend/packaging/`.

## Build, Test, and Development Commands

- `bun install`: install all workspace dependencies from the root `bun.lock`.
- `sudo ./tools/tako-host setup`: one-time host install of PAM, sysusers, and systemd units pointed at this checkout.
- `bun run dev`: host-integrated stack (real sessiond/PAM) plus `vite build --watch`; open https://127.0.0.1:9090.
- `bun run dev:ui`: passwordless `serve --dev` + Vite on :5173 (no PAM/PTY; UI-only).
- `bun run build`: type-check and build the dashboard, then produce the stripped `bin/tako` executable.
- `bun run test`: run Go tests and dashboard type checking through Nx.
- `bun run lint`: run `go vet` and ESLint through Nx.
- `bun run race`: run the full Go race suite before security-sensitive changes.
- `bun nx <target> <project>`: run an individual Nx target, such as `bun nx test backend`.

## Coding Style & Naming Conventions

Format Go with `gofmt`; use short lowercase package names and exported PascalCase identifiers. Keep HTTP handlers and platform adapters focused and context-aware. Go tests use `*_test.go` and `TestBehavior` names.

Use TypeScript, two-space indentation, PascalCase React components, and `kebab-case.tsx` filenames. Run `bun nx format dashboard`, `bun nx typecheck dashboard`, and `bun nx lint dashboard` from the repository root. Prefer existing shadcn/Base UI components over bespoke controls.

## Testing Guidelines

Place tests beside implementation. Cover parsers, bounded streams, session expiry, authorization boundaries, bridge framing, and cancellation. API changes should include handler tests and update `apps/backend/api/openapi.yaml`. UI changes should exercise loading, error, empty, keyboard, mobile, and dark-mode states.

## Commit & Pull Request Guidelines

Git history is unavailable in this checkout. Use concise imperative subjects, optionally Conventional Commit prefixes such as `feat:`, `fix:`, or `docs:`. Keep commits scoped. Pull requests should explain behavior and security impact, list verification commands, link relevant issues/ADRs, and include screenshots for visible UI changes.

## Security & Configuration

Never move privileged work into the network-facing gateway. Preserve separate process modes and systemd sandboxes. Do not store passwords, session tokens, certificates, or local configuration in Git; start from `apps/backend/config.example.toml`.
