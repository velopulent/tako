# 41 — Model network ownership and configuration

**What to build:** Interfaces, addresses, routes, DNS, profiles, active owner, and truthful read-only degraded states.

**Blocked by:** 03 — Enrich capability reporting; 04 — Correct page titles and scoped metrics

**Status:** completed

- [x] Ownership probes NetworkManager, Netplan, networkd/ifupdown, and conflicts rather than distro name.
- [x] Per-interface receive/transmit selection uses relevant bounded metrics.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
