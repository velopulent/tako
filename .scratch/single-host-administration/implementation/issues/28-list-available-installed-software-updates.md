# 28 — List available installed-software updates

**What to build:** PackageKit capability-aware update inventory plus version-gated APT/DNF fallbacks.

**Blocked by:** 03 — Enrich capability reporting

**Status:** ready-for-agent

- [ ] Adapter selection probes roles/contracts/version and reports external locks.
- [ ] Updates show versions, severity when available, size, and details without general package management.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
