# ADR 0004: Embedded dashboard

Status: accepted

The dashboard is the `apps/dashboard` Nx application, a Vite React SPA using TanStack Router, TanStack Query, Tailwind, and shadcn/ui Base UI. Bun owns the root JavaScript workspace and lockfile. Nx infers Vite and ESLint tasks from their native configuration files. Production builds write hashed assets into `apps/backend/internal/dashboard/dist`, and `go:embed` produces self-contained binaries. The backend build depends on the dashboard build so embedded assets cannot be skipped.
