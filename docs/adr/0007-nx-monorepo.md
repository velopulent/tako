# ADR 0007: Nx monorepo orchestration

Status: accepted

Tako keeps deployable applications under `apps/`: `apps/backend` contains the Go module and deployment assets, while `apps/dashboard` contains the React application. Bun manages the root JavaScript workspace and lockfile. A root Go workspace points to the backend module for editor and command-line discovery.

Nx is the repository task entry point. Official inference plugins derive dashboard build, development, preview, and lint tasks from Vite and ESLint configuration. Explicit `project.json` targets wrap Go commands and dashboard type checking. The backend declares the dashboard as a dependency because its production binary embeds dashboard build output.

Tool-specific configuration remains authoritative. Nx adds project discovery, task dependencies, affected execution, and local caching without replacing Go, Vite, TypeScript, ESLint, or Bun configuration.
