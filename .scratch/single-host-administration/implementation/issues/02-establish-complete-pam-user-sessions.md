# 02 — Establish complete PAM user sessions

**What to build:** Bidirectional PAM/MFA conversation, NSS identity resolution, complete PAM lifecycle/environment, and an authenticated User bridge.

**Blocked by:** None — can start immediately

**Status:** completed

- [x] Multiple prompt types and rounds work with strict bounds and cancellation.
- [x] Session close removes credentials, closes PAM state, and terminates User bridge.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.

**Verification:** `bun run test --skip-nx-cache`, `bun run lint
--skip-nx-cache`, `bun run race --skip-nx-cache`, and the opt-in
`go test -tags integration ./internal/auth ./internal/sessiond` seam pass. The
integration tests run their real PAM/NSS/User-bridge assertions when disposable
VM credentials and a built Tako binary are supplied.
