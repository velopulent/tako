# 09 — Package production access foundations

**What to build:** Distribution PAM stacks, Polkit action, optional exact-command sudoers guidance, hardened systemd units, TLS/reverse-proxy guidance, and packaged smoke test.

**Blocked by:** 02 — Establish complete PAM user sessions; 05 — Secure Administrative access through sudo and Polkit

**Status:** ready-for-agent

- [ ] Packages preserve Gateway, Session service, User bridge, and Privileged bridge isolation.
- [ ] Clean-VM smoke tests authenticate, request Administrative access, and log out on each certified image.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
