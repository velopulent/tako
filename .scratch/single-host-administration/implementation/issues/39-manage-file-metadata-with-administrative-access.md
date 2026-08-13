# 39 — Manage file metadata with Administrative access

**What to build:** Permissions, ownership, and protected-path browsing/writes while treating Administrative Files as full-root equivalent.

**Blocked by:** 05 — Secure Administrative access through sudo and Polkit; 32 — Create rename move and copy Files

**Status:** ready-for-agent

- [ ] UI explicitly communicates full-root authority and access ends with root bridge.
- [ ] Metadata/path mutations use descriptor-relative operations, confirmation, verification, and audit.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
