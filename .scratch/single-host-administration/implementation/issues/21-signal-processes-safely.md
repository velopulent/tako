# 21 — Signal processes safely

**What to build:** pidfd or verified-start-time signals under correct UNIX authority with tree-impact confirmation.

**Blocked by:** 06 — Introduce typed privileged operations; 20 — Inspect process relationships and resources

**Status:** ready-for-agent

- [ ] Same-user and other-user authority paths are distinct and audited.
- [ ] Tree actions preview exact members and reject changed identities.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
