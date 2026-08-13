# 05 — Secure Administrative access through sudo and Polkit

**What to build:** A short-lived root bridge authorized by host policy, supporting promptless policy, `NOPASSWD`, and interactive MFA.

**Blocked by:** 02 — Establish complete PAM user sessions

**Status:** in-progress

- [x] Gateway cannot grant elevation by reauthenticating an otherwise unauthorized user.
- [x] Drop, logout, timeout, bridge loss, and session loss independently terminate Administrative access.
- [ ] Dedicated browser multi-round MFA elevation UX and VM policy matrix are still pending; the root policy path is covered by unit/integration seams.
