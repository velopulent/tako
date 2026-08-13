# 08 — Run durable diagnostic inventory jobs

**What to build:** Reusable durable jobs, progress, cancellation, reconnect, restart recovery, and Jobs UI through a useful host-inventory report.

**Blocked by:** 03 — Enrich capability reporting; 07 — Show durable operation receipts

**Status:** completed

- [x] Job state survives navigation and restart; cancellation semantics are explicit.
- [x] Dangerous interrupted work never retries silently.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.

Implementation notes: SQLite migration 3 stores bounded diagnostic jobs and marks pending/running jobs as `interrupted` during startup recovery. A two-worker, 32-entry queue runs host-inventory reports with durable progress, a 45-second execution bound, explicit cancellation, reconnectable `GET /jobs` and `GET /jobs/{id}` observation, and no automatic retry. The dashboard Jobs page polls only active jobs and explains interrupted state. Host-inventory output contains host identity and capability metadata only; credentials, grants, and raw command output are not persisted.
