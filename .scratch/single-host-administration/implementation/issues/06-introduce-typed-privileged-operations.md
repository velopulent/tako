# 06 — Introduce typed privileged operations

**What to build:** A typed operation catalog with exact schemas, root-side validation, deadlines, cancellation, and journald audit.

**Blocked by:** 05 — Secure Administrative access through sudo and Polkit

**Status:** completed

- [x] No shell, arbitrary executable, root terminal, or generic privileged RPC is reachable.
- [x] Every operation revalidates actor, target, arguments, authority, and limits at root boundary.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
