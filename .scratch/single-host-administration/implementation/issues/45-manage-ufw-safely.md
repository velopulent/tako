# 45 — Manage UFW safely

**What to build:** Version-gated UFW inspection and fixed-grammar mutations with independent rollback and access warnings.

**Blocked by:** 06 — Introduce typed privileged operations; 08 — Run durable diagnostic inventory jobs; 41 — Model network ownership and configuration

**Status:** ready-for-agent

- [ ] Human output parsers are version-gated/fail-closed; dry-run is not treated as a transaction.
- [ ] Enable/default/rule changes preserve management access and require fresh reconnection confirmation.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
