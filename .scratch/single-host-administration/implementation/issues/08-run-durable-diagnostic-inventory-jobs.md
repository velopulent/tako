# 08 — Run durable diagnostic inventory jobs

**What to build:** Reusable durable jobs, progress, cancellation, reconnect, restart recovery, and Jobs UI through a useful host-inventory report.

**Blocked by:** 03 — Enrich capability reporting; 07 — Show durable operation receipts

**Status:** ready-for-agent

- [ ] Job state survives navigation and restart; cancellation semantics are explicit.
- [ ] Dangerous interrupted work never retries silently.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
