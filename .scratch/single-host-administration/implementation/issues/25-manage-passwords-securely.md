# 25 — Manage passwords securely

**What to build:** Self-service password change and administrative reset without secret persistence or argv exposure.

**Blocked by:** 02 — Establish complete PAM user sessions; 06 — Introduce typed privileged operations; 23 — Manage local account lifecycle

**Status:** completed

- [x] Secrets use a bounded PAM conversation and never enter SQLite, logs, jobs, receipts, argv, or environment.
- [x] Expired-password and policy failures receive distinct safe errors.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
