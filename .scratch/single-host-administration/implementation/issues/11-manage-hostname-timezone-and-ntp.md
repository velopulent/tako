# 11 — Manage hostname, timezone, and NTP

**What to build:** Previewed and verified host configuration through hostname1 and timedate1.

**Blocked by:** 06 — Introduce typed privileged operations; 10 — Expand host inventory

**Status:** completed

- [x] Stale or unauthorized changes are rejected and successful changes produce receipts.
- [x] UI displays current state, impact, progress, error, and refreshed result.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.

Implementation notes: hostname1/timedate1 reads and writes stay behind the privileged session boundary. The HTTP preview and update paths use a SHA-256 current-state fingerprint, strict request limits, CSRF, Administrative access, post-write verification, and durable operation receipts. The Host page exposes read-only state, preview impact, stale conflicts, loading/error states, and keyboard/mobile/dark-mode seams.
