# ADR 0015: Bounded live journal workflows

## Context

Live journal output can arrive while an operator is reading older evidence. A
stream that always prepends rows makes investigation jump and an unbounded
browser buffer eventually consumes memory. Export also needs to preserve the
same authorization and filters as the journal query without copying records to
durable storage.

## Decision

Tako follows journald through a filtered, cancellable SSE subprocess. The
gateway applies the same validated boot, time, priority, unit, executable,
message, and detail filters as history queries. The browser keeps at most 500
live rows. It prepends incoming rows only while at the latest page; otherwise it
counts at most 500 pending rows and offers an explicit jump-to-latest action.

Journal rows can request a bounded allowlist of structured diagnostic fields on
demand. Filtered exports are authenticated, capped at 500 rows and 8 MiB, and
are emitted as CSV or JSON directly from the live query result. Journal data is
never persisted in SQLite.

## Consequences

Operators can pause or resume following, inspect older pages without forced
scrolling, and share an exact filtered export. A concurrent journal vacuum can
still make a page shorter; the UI keeps pagination and stream failures
visible rather than silently retrying or buffering indefinitely.
