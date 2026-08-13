# 03 — Enrich capability reporting

**What to build:** Backend/version, read and mutation authority, rollback support, missing dependency, contract, and degraded-mode reasons in API and UI.

**Blocked by:** None — can start immediately

**Status:** completed

- [x] Capabilities come from runtime probes rather than distribution name alone.
- [x] Unavailable or conflicted backends fail closed with setup guidance.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.

**Verification:** `bun run test --skip-nx-cache` and `bun run lint
--skip-nx-cache` pass. Runtime probes are cached, single-flight, deadline-bound,
and output-bound. `go test -tags integration ./internal/platform` provides the
supported-distribution VM contract seam.
