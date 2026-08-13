# 26 — Manage authorized SSH keys

**What to build:** Self-service and Administrative access SSH-key management through safe filesystem semantics.

**Blocked by:** 23 — Manage local account lifecycle; 31 — Browse Files under UNIX authority

**Status:** completed

- [x] Keys are validated, atomically saved, permission-correct, and stale-write protected.
- [x] Protected-path authority and receipts are explicit.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
