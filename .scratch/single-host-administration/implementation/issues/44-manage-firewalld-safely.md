# 44 — Manage firewalld safely

**What to build:** Zones, services, ports, sources, and policies with runtime verification before persistence.

**Blocked by:** 06 — Introduce typed privileged operations; 08 — Run durable diagnostic inventory jobs; 41 — Model network ownership and configuration

**Status:** ready-for-agent

- [ ] Tako protects SSH/Tako access and keeps runtime/permanent state explicit.
- [ ] Conflicting firewall ownership fails closed and deprecated direct rules are not exposed.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
