# ADR 0012: Structured systemd timer pairs

## Context

Timer editing must not expose arbitrary unit-file text or shell execution to the
gateway. A timer is also inseparable from its oneshot service, and two browser
sessions must not silently overwrite one another.

## Decision

Tako accepts a small structured timer operation (`scope`, unit stem, one
schedule, absolute command, and persistence flag). The platform adapter writes
the matching `.timer` and `.service` files with bounded, atomic file writes and
uses a SHA-256 fingerprint of both files for optimistic concurrency. Enable and
disable only manage the allowlisted `timers.target.wants` link. System scope is
handled by sessiond; user scope is handled by the authenticated user bridge.
The network gateway never writes unit files or invokes systemctl directly.

Preview is read-only. Mutating operations require the current fingerprint for
existing pairs, and all operation/request payloads reject unknown fields,
unsafe unit names, shell metacharacters, specifier expansion, and ambiguous
schedules. User-visible timer forms use the same structure and surface stale
conflicts as a refresh-required error.

## Consequences

The first version supports the common calendar/relative schedules and oneshot
commands while leaving advanced systemd directives for a later, explicitly
validated extension. A systemd daemon reload is performed only after a
successful write. VM coverage can exercise the same sessiond adapter without
granting the HTTP process additional privilege.
