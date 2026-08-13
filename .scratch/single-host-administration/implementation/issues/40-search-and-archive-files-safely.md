# 40 — Search and archive Files safely

**What to build:** Bounded filename search plus safe archive creation and extraction.

**Blocked by:** 08 — Run durable diagnostic inventory jobs; 32 — Create rename move and copy Files

**Status:** ready-for-agent

- [ ] Extraction rejects absolute/parent paths and enforces entry, byte, depth, and link limits.
- [ ] Search/archive jobs support progress, cancellation, and recovery without full-content indexing.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
