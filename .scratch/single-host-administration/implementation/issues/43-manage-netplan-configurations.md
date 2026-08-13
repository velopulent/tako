# 43 — Manage Netplan configurations

**What to build:** First-class Netplan D-Bus editing with timed Try, verification, and rollback.

**Blocked by:** 06 — Introduce typed privileged operations; 08 — Run durable diagnostic inventory jobs; 41 — Model network ownership and configuration

**Status:** completed

- [x] Tako identifies Netplan ownership and never generically overwrites unmanaged YAML.
- [x] Rollback verifies runtime and on-disk state before reporting recovery.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
