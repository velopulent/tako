# 16 — Manage safe systemd overrides

**What to build:** Drop-in environment/settings editing with stale detection, daemon reload, and recovery guidance.

**Blocked by:** 13 — Harden existing service actions

**Status:** completed

- [x] Only supported override fields are editable; vendor unit remains read-only.
- [x] Apply atomically writes only Tako's drop-in, reloads systemd through the correct bridge, verifies the resulting managed state, and records a receipt.
- [x] API changes, user-visible behavior, and applicable unit/UI/integration seams are tested and documented.
