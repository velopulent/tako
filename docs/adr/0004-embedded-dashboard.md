# ADR 0004: Embedded dashboard

Status: accepted

The dashboard is a Vite React SPA using TanStack Router, TanStack Query, Tailwind, and shadcn/ui Base UI. Bun owns JavaScript dependencies. Production builds write hashed assets into the Go web UI package and `go:embed` produces self-contained binaries.

