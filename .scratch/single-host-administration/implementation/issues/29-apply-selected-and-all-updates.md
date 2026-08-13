# 29 — Apply selected and all updates

**What to build:** Serialized update jobs with advisory simulation, preview, progress, cancellation policy, and reconnect.

**Blocked by:** 06 — Introduce typed privileged operations; 08 — Run durable diagnostic inventory jobs; 28 — List available installed-software updates

**Status:** completed

- [x] Package-manager locks are respected and dangerous interruption never silently retries.
- [x] Selected/all workflows verify final state and produce sanitized receipts.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
