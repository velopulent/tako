# ADR 0026: Serialized software update jobs

Status: accepted

## Decision

Tako applies selected or all installed-software updates through a durable,
serialized job. The browser first previews the current inventory and submits
the inventory fingerprint with an explicit `APPLY UPDATES` confirmation. The
privileged session boundary rechecks the fingerprint and advisory package
manager lock immediately before invoking the selected PackageKit, APT, or DNF
adapter.

Durable job parameters contain only the validated operation. The administrative
grant token stays in gateway memory and is removed when the job finishes or is
canceled; it is never persisted or included in receipts. Jobs expose bounded
progress, reconnect through the existing job endpoint, and cancellation stops
the context-bound command without automatic retry. A successful command must
also pass a fresh inventory verification before the job is marked succeeded.

## Consequences

- A package operation may partially change the host before cancellation or a
  verification failure; Tako reports that state and never silently retries it.
- Only one update job runs at a time, preventing concurrent package-manager
  mutations while allowing the rest of the dashboard to remain responsive.
- Package-manager output is not exposed in the API or receipts; operators get
  sanitized status, progress, and failure categories instead.
