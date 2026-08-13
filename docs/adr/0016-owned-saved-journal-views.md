# ADR 0016: Owned saved journal views

## Context

Operators repeatedly investigate the same unit, priority, or incident text.
Re-entering filters is slow, but journal records themselves remain Linux-owned
and must not be copied into Tako's database. Saved filters also need a clear
ownership boundary and stale-write protection.

## Decision

Tako stores only validated journal filter definitions in SQLite. Each saved
view has an opaque ID, an owner username, a unique owner-local name, a revision,
and timestamps. List, update, and delete operations always constrain the owner
in SQL; a user cannot read or mutate another user's view. Updates and deletes
require the current revision and return conflict on stale writes. Supported
filters are boot, RFC3339 time bounds, priority, unit, executable, literal text,
and bounded detail-field selection.

The dashboard can save the current filters, apply a view, rename it, update it
with current filters, or delete it. Journal entries and credentials are never
stored in the saved-view table.

## Consequences

Saved views survive restart and remain portable as filter definitions. Shared
team views are intentionally deferred; a future sharing policy can add explicit
ACLs without weakening the owner-only default.
