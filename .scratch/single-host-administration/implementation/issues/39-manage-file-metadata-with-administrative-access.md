# 39 — Manage file metadata with Administrative access

**What to build:** Permissions, ownership, and protected-path browsing/writes while treating Administrative Files as full-root equivalent.

**Blocked by:** 05 — Secure Administrative access through sudo and Polkit; 32 — Create rename move and copy Files

**Status:** completed

- [x] UI explicitly communicates full-root authority and access ends with root bridge.
- [x] Metadata/path mutations use privileged root-session authority, confirmation, verification, and audit.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
