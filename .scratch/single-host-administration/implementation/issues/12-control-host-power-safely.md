# 12 — Control host power safely

**What to build:** Capability-aware reboot and shutdown with inhibitor details, typed confirmation, and receipts.

**Blocked by:** 06 — Introduce typed privileged operations; 10 — Expand host inventory

**Status:** completed

- [x] Unavailable, denied, challenged, and inhibited states are distinct.
- [x] Power requests are tested without allowing arbitrary logind operations.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.

Implementation notes: logind `CanReboot`, `CanPowerOff`, and inhibitor state are exposed read-only with a fingerprint. Reboot/shutdown cross the privileged session boundary as a typed operation, require Administrative access, exact `REBOOT`/`SHUTDOWN` confirmation, a fresh fingerprint, and CSRF. The UI distinguishes unavailable, denied, challenged, and inhibited states; successful and failed requests are recorded as sanitized receipts.
