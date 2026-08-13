# ADR 0013: Safe systemd service overrides

## Context

Operators need common service customization without replacing vendor unit files
or receiving an arbitrary unit-file editor. Override edits must work under user
and system scopes, detect concurrent edits, and recover cleanly when a service
fails after a change.

## Decision

Tako manages one allowlisted drop-in, `50-tako.conf`, per service. The typed
operation permits environment entries, restart policy/delay, startup/shutdown
timeouts, nice, CPU quota, memory limit, and task limit. Unit names, keys,
values, durations, and numeric ranges are bounded; unsupported directives make
the managed drop-in unreadable rather than silently adopting it. Vendor files
remain read-only.

The platform adapter writes the drop-in atomically and fingerprints its exact
bytes. Existing drop-ins require that fingerprint for apply/delete, while
preview is read-only. Sessiond performs system-scope writes and asks the user
bridge to perform user-scope writes; each reloads the matching systemd manager
only after a successful write. The API records sanitized operation receipts and
returns recovery guidance.

## Consequences

The override surface is intentionally narrower than systemd's full directive
set, but it is reviewable and portable. Operators can remove a managed drop-in
without touching vendor content. Additional directives require a new typed
field, parser, tests, and documented safety rationale.
