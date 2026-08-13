# 28 — List available installed-software updates

**What to build:** PackageKit capability-aware update inventory plus version-gated APT/DNF fallbacks.

**Blocked by:** 03 — Enrich capability reporting

**Status:** completed

- [x] Adapter selection probes roles/contracts/version and reports external locks.
- [x] Updates show versions, severity when available, size, and details without general package management.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
