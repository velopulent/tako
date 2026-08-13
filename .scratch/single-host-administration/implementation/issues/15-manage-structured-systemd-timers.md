# 15 — Manage structured systemd timers

**What to build:** Safe creation, editing, enabling, disabling, and deletion of timer/service pairs.

**Blocked by:** 13 — Harden existing service actions

**Status:** ready-for-agent

- [ ] Structured validation prevents arbitrary unit injection and stale edits conflict.
- [ ] Timer workflows are end-to-end tested in VM integration.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
