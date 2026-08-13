# 15 — Manage structured systemd timers

**What to build:** Safe creation, editing, enabling, disabling, and deletion of timer/service pairs.

**Blocked by:** 13 — Harden existing service actions

**Status:** completed

- [x] Structured validation prevents arbitrary unit injection and stale edits conflict.
- [x] Timer workflows have bounded backend, bridge, sessiond, controlled HTTP/UI seams, and an opt-in disposable-VM systemd integration test.
- [x] API changes, user-visible behavior, and applicable unit/UI/integration seams are tested and documented.
