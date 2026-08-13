# 35 — Edit text Files atomically

**What to build:** Text editing up to configurable 5 MiB with atomic save, version conflicts, and deliberate overwrite.

**Blocked by:** 32 — Create rename move and copy Files

**Status:** completed

- [x] MIME/text classification and size limits are enforced server-side.
- [x] Save uses same-directory atomic replacement, metadata handling, fsync, and stale token.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
