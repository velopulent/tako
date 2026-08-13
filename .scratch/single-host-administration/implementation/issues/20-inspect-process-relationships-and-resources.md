# 20 — Inspect process relationships and resources

**What to build:** Process trees, cgroups, open files, sockets, and bounded resource history.

**Blocked by:** 03 — Enrich capability reporting

**Status:** ready-for-agent

- [ ] Process identity includes start time to distinguish PID reuse.
- [ ] Permission-denied details degrade per process without failing whole inventory.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
