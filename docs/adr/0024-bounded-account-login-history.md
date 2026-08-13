# ADR 0024: Read account login history directly from journald

Status: accepted

## Decision

Tako exposes a bounded, read-only account login/session history query backed by
the host journal. The gateway applies account authorization, time/outcome
filters, opaque journal cursors, and a 100-event page bound. It classifies only
well-known authentication and session messages, returning event, outcome,
service, remote address, and session identifiers rather than arbitrary journal
text.

The response includes whether the requested identity is local, NSS-backed, or
deleted/unknown. This keeps historical records understandable after an account
is removed or when it comes from a remote NSS provider. Journal data is never
persisted in Tako's SQLite preferences database.

## Consequences

- Journal availability and permissions can make history temporarily
  unavailable; the API reports that explicitly.
- Classification is intentionally conservative, so unrelated journal lines
  are omitted instead of being presented as login events.
- The dashboard can inspect the signed-in account, while Administrative access
  is required for another or deleted identity.
