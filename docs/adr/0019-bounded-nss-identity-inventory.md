# ADR 0019: Bounded NSS identity inventory

## Context

NSS can combine local files with remote identity providers. Reading only
`/etc/passwd` and `/etc/group` hides accounts that can authenticate, while
allowing remote entries to appear mutable would imply unsupported account
management authority. NSS providers can also be slow or return unexpectedly
large results.

## Decision

The gateway obtains users and groups through bounded `getent passwd` and
`getent group` calls using the request context. Output, line length, record
count, and group membership count are capped. Local names are identified from
`/etc/passwd` and `/etc/group`; entries not present there are marked
`nss-read-only`, with `mutable: false` and an explanatory reason. Local
entries are marked `local` and `mutable`, while user records include both
supplemental and primary group names.

The `/users` and `/groups` endpoints expose the same explicit source and
mutability contract. Remote identity management is intentionally deferred to
the account and group lifecycle adapters, which must reject non-local entries.

## Consequences

Operators see the identities NSS can resolve without granting remote account
providers local mutation authority. Slow or oversized providers fail with a
bounded error rather than consuming unbounded gateway memory or request
workers. Inventory can be incomplete when a provider exceeds the documented
limits, but the failure is explicit and retryable.
