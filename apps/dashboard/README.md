# Tako dashboard

Embedded React frontend for Tako. This Nx application uses Vite, TanStack Router, TanStack Query, shadcn/ui Base UI, Tailwind, and Recharts.

## Commands

```bash
bun nx dev dashboard
bun nx typecheck dashboard
bun nx lint dashboard
bun nx build dashboard
```

Run commands from repository root after `bun install`. Production builds write directly to `apps/backend/internal/dashboard/dist` for Go embedding. Run shadcn CLI commands from this directory with `bunx --bun shadcn@latest`.
