# 13 — Harden existing service actions

**What to build:** Existing service lifecycle actions under correct system or user bridge authority.

**Blocked by:** 06 — Introduce typed privileged operations; 07 — Show durable operation receipts

**Status:** ready-for-agent

- [ ] System actions require authorized Privileged bridge; user actions run as authenticated UNIX identity.
- [ ] Validated inventory unit names and action allowlists prevent arbitrary targets.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
