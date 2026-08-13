# 31 — Browse Files under UNIX authority

**What to build:** Safe directory navigation, locked entries, hidden-file preference, symlink metadata, and discoverable shadcn action surfaces.

**Blocked by:** 02 — Establish complete PAM user sessions; 03 — Enrich capability reporting

**Status:** completed

- [x] Directory children are never leaked without authority; filesystem I/O runs only in User bridge.
- [x] Files UI is split into focused components with toolbar, row menu, keyboard, mobile, and dark states.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
