# 09 — Package production access foundations

**What to build:** Distribution PAM stacks, Polkit action, optional exact-command sudoers guidance, hardened systemd units, TLS/reverse-proxy guidance, and packaged smoke test.

**Blocked by:** 02 — Establish complete PAM user sessions; 05 — Secure Administrative access through sudo and Polkit

**Status:** completed

- [x] Packages preserve Gateway, Session service, User bridge, and Privileged bridge isolation.
- [x] Clean-VM smoke seam authenticates and verifies service/socket identities; the VM harness supplies PAM credentials for the optional login path.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.

Implementation notes: hardened systemd units, Debian/Ubuntu and Fedora/RHEL PAM transforms, the Tako Polkit action, exact-command sudoers guidance, reverse-proxy deployment notes, and `packaging/smoke-test.sh` are included. The smoke script is intentionally run only inside a disposable installed VM; it does not mutate the host or persist credentials.
