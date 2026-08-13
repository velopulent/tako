# 48 — Remediate AppArmor findings narrowly

**What to build:** Profile state controls and validated profile loading with version/schema warnings.

**Blocked by:** 06 — Introduce typed privileged operations; 46 — Inspect active security policy framework

**Status:** ready-for-agent

- [ ] Malformed/unsupported JSON and feature downgrades fail closed or show explicit uncertainty.
- [ ] Only one resolved allowlisted profile is mutated per confirmed action.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
