# 40 — Search and archive Files safely

**What to build:** Bounded filename search plus safe archive creation and extraction.

**Blocked by:** 08 — Run durable diagnostic inventory jobs; 32 — Create rename move and copy Files

**Status:** completed

- [x] Extraction rejects absolute/parent paths and enforces entry, byte, depth, and link limits.
- [x] Search/archive operations are bounded and cancellation-aware without full-content indexing; durable jobs remain reserved for work that can safely retain live UNIX authority.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
