# Tako dashboard

Embedded React frontend for Tako. Uses Vite, TanStack Router, TanStack Query, shadcn/ui Base UI, Tailwind, and Recharts.

## Commands

```sh
bun install
bun run dev
bun run typecheck
bun run lint
bun run build
```

Production builds write directly to `../internal/webui/dist` for Go embedding. Use `bunx --bun shadcn@latest` for shadcn CLI commands.
