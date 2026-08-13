# Tako backend

Go backend for Tako. One multicall executable provides `serve`, `sessiond`, and `bridge` modes. Dashboard production assets are embedded from `internal/dashboard/dist`.

Run tasks from repository root through Nx:

```sh
bun nx dev backend
bun nx test backend
bun nx lint backend
bun nx race backend
bun nx build backend
```

`backend:build` first builds `dashboard`, then writes executable to `bin/tako`.
