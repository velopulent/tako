# 02 — Establish complete PAM user sessions

**What to build:** Bidirectional PAM/MFA conversation, NSS identity resolution, complete PAM lifecycle/environment, and an authenticated User bridge.

**Blocked by:** None — can start immediately

**Status:** ready-for-agent

- [ ] Multiple prompt types and rounds work with strict bounds and cancellation.
- [ ] Session close removes credentials, closes PAM state, and terminates User bridge.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
