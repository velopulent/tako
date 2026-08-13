# 01 — Persist server monitoring preference

**What to build:** A migrated SQLite store plus one real server-side monitoring preference, restart persistence, and Settings UI.

**Blocked by:** None — can start immediately

**Status:** completed

- [x] Preference survives restart and is shared across browsers.
- [x] Migration and HTTP/UI behavior are tested without persisting secrets.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.

**Verification:** `bun run test`, `bun run lint`, and `bun run race` pass. This
slice has no Linux-VM seam because it stores Tako-owned data and uses no host
platform adapter.
