# 05 — Secure Administrative access through sudo and Polkit

**What to build:** A short-lived root bridge authorized by host policy, supporting promptless policy, `NOPASSWD`, and interactive MFA.

**Blocked by:** 02 — Establish complete PAM user sessions

**Status:** completed

- [x] Gateway cannot grant elevation by reauthenticating an otherwise unauthorized user.
- [x] Drop, logout, timeout, bridge loss, and session loss independently terminate Administrative access.
- [x] Dedicated browser multi-round MFA elevation UX accepts a bounded additional PAM response, and the supported policy families are documented in the VM release matrix.
