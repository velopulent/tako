# 47 — Remediate SELinux findings narrowly

**What to build:** Known booleans, labels, restorecon, and previewed narrow remediation without automatic audit2allow.

**Blocked by:** 06 — Introduce typed privileged operations; 46 — Inspect active security policy framework

**Status:** ready-for-agent

- [ ] Tako never presents disabling enforcement or broad generated allow rules as routine fix.
- [ ] Every mutation verifies resulting enforcement/label state and records receipt.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
