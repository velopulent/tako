# 32 — Create rename move and copy Files

**What to build:** Descriptor-relative create, rename, move, copy, and paste with stale protection and progress.

**Blocked by:** 07 — Show durable operation receipts; 31 — Browse Files under UNIX authority

**Status:** completed

- [x] Operations resist traversal/symlink races and handle cross-filesystem behavior explicitly.
- [x] Postconditions and receipts reflect actual filesystem result.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
