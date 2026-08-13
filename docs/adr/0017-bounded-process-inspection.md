# ADR 0017: Bounded process inspection

## Context

The process list is useful for triage only when PID reuse is handled and a
single unreadable `/proc` entry does not hide the rest of the host. Operators
also need relationships and resource context without turning the gateway into
a privileged process inspector.

## Decision

Tako treats `(pid, /proc/<pid>/stat starttime)` as the process identity. Detail
requests carry the starttime and return conflict when the PID has been reused.
The unprivileged procfs adapter reports readable data and per-process access
warnings independently. Detail data is bounded to 256 open files, 256 sockets,
64 KiB cgroup content, and a 120-sample in-memory resource history. Parent and
children are derived from the current inventory; Linux remains authoritative.

The dashboard opens an authenticated detail sheet from a keyboard-accessible
process row and keeps loading, error, empty, and restricted-data states
visible. No process data is persisted in SQLite and no process mutation is
introduced by this ticket.

## Consequences

Short-lived processes can disappear between inventory and detail requests, and
permission-restricted files remain unavailable with an explanation. Resource
history starts when the process inventory is refreshed and is discarded on
restart.
