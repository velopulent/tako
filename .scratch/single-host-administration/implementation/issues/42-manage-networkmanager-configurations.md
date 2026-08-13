# 42 — Manage NetworkManager configurations

**What to build:** DHCP/static IP, DNS, routes, bonds, bridges, VLANs, Wi-Fi, timed checkpoints, and reconnect commit through NetworkManager.

**Blocked by:** 06 — Introduce typed privileged operations; 08 — Run durable diagnostic inventory jobs; 41 — Model network ownership and configuration

**Status:** completed

- [x] Every remote mutation arms rollback before change and commits only from a fresh authenticated connection.
- [x] D-Bus capabilities/permissions and postconditions gate available controls.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
