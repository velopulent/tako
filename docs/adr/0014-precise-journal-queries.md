# ADR 0014: Precise bounded journal queries

## Context

The journal is the host audit authority, but a browser cannot safely load an
unbounded or localized human-readable stream. Operators need repeatable filters
and pagination for incident investigation.

## Decision

Tako invokes `journalctl` with structured JSON output and bounded arguments for
boot, time range, priority, unit, executable, and literal message filters.
Output is read through a fixed 8 MiB limit and the subprocess inherits request
cancellation. Journal cursors are base64url-wrapped before crossing the API so
clients treat them as opaque; the backend validates and decodes them, and the
cursor never appears in public journal entries. The page is capped at 500
entries.

The dashboard uses debounced server-side filters, bounded live-follow memory,
loading/error/empty states, and an explicit older-page action. It retains
client-side search only for the already loaded page.

## Consequences

Pagination depends on systemd journal cursor semantics and may return fewer
entries when records are concurrently vacuumed. Human-localized output is never
parsed. Export and saved views can build on the same query contract later.
