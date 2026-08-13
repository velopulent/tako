# 05 — Secure Administrative access through sudo and Polkit

**What to build:** A short-lived root bridge authorized by host policy, supporting promptless policy, `NOPASSWD`, and interactive MFA.

**Blocked by:** 02 — Establish complete PAM user sessions

**Status:** ready-for-agent

- [ ] Gateway cannot grant elevation by reauthenticating an otherwise unauthorized user.
- [ ] Drop, logout, timeout, bridge loss, and session loss independently terminate Administrative access.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
