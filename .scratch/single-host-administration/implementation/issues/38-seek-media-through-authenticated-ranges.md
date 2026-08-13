# 38 — Seek media through authenticated ranges

**What to build:** Conditional and HTTP Range serving with exact-file/version authorization on every request.

**Blocked by:** 37 — Preview media securely

**Status:** ready-for-agent

- [ ] Valid, suffix, open-ended, invalid, unsatisfiable, conditional, and If-Range requests behave correctly.
- [ ] Responses provide correct 206, Accept-Ranges, Content-Range, MIME, and safe disposition semantics.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
