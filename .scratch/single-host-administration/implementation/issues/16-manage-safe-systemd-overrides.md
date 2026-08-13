# 16 — Manage safe systemd overrides

**What to build:** Drop-in environment/settings editing with stale detection, daemon reload, and recovery guidance.

**Blocked by:** 13 — Harden existing service actions

**Status:** ready-for-agent

- [ ] Only supported override fields are editable; vendor unit remains read-only.
- [ ] Apply verifies resulting systemd state and records receipt.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
