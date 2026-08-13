# 46 — Inspect active security policy framework

**What to build:** SELinux/AppArmor status, capabilities, denials, and contextual explanations.

**Blocked by:** 03 — Enrich capability reporting; 17 — Query journal history precisely

**Status:** completed

- [x] Kernel and userspace capability are probed separately and structured/raw audit formats are bounded.
- [x] Hosts without active framework degrade cleanly.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
