# Tako backend

Go backend for Tako. One multicall executable provides `serve`, `sessiond`, and `bridge` modes. Dashboard production assets are embedded from `internal/dashboard/dist`. Host development can overlay that directory via `/run/tako/dashboard` (see root [`HACKING.md`](../../HACKING.md)).

Run tasks from repository root through Nx:

```sh
bun run dev
bun run dev:ui
bun nx test backend
bun nx lint backend
bun nx race backend
bun nx build backend
```

`backend:build` first builds `dashboard`, then writes executable to `bin/tako`.
