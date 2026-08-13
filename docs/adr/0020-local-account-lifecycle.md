# ADR 0020: Protected local account lifecycle

## Context

NSS inventory includes remote identities that Tako cannot safely mutate. Local
account changes also affect authentication and may remove a user's home
directory, so a web request must not become an arbitrary root command or race
another administrator's change.

## Decision

Local account management uses a typed operation vocabulary (`create`, `update`,
`lock`, `unlock`, and `delete`) across the gateway and the root-owned session
service. The session service invokes only fixed shadow-utils executables with
allowlisted arguments, bounded output, a request deadline, and no shell. It
re-reads NSS state and `passwd -S` after every write and rejects failed
postconditions.

Existing accounts carry a SHA-256 state fingerprint. Apply operations require
that fingerprint and are rejected when the account changed since preview.
Remote/NSS-only identities are explicitly read-only. Delete and lock require
typed confirmation and the current operator account cannot be locked or
deleted. The dashboard exposes loading/error states, impact warnings, a
preview-first flow, and administrative/read-only badges.

## Consequences

Account writes remain isolated from the network-facing gateway and are
auditable through operation receipts. NSS caching or provider lag can make
postcondition verification fail visibly, requiring a retry instead of silently
claiming success. Password setup is intentionally left to the terminal so
credentials never enter HTTP request bodies or application storage.
