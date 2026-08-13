# 10 — Expand host inventory

**What to build:** Hardware, boot history, shutdown cleanliness, restart-required state, and richer Overview.

**Blocked by:** 03 — Enrich capability reporting

**Status:** completed

- [x] Missing optional sources degrade independently with clear capability reasons.
- [x] Host inventory is bounded and tested at HTTP and real-system seams.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.

Implementation notes: the dashboard host snapshot now includes procfs hardware details, boot identity, previous-shutdown evidence from bounded journald reads, and restart-required providers for Debian-family and RPM-family hosts. Each optional source carries an availability/reason field; command output is capped and every probe has a timeout. Overview renders these states independently and keeps degraded data visible.
