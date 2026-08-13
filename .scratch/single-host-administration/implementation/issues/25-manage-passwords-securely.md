# 25 — Manage passwords securely

**What to build:** Self-service password change and administrative reset without secret persistence or argv exposure.

**Blocked by:** 02 — Establish complete PAM user sessions; 06 — Introduce typed privileged operations; 23 — Manage local account lifecycle

**Status:** ready-for-agent

- [ ] Secrets use bounded conversation or protected stdin and never enter SQLite, logs, jobs, receipts, or environment.
- [ ] Expired-password and policy failures receive distinct safe errors.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
