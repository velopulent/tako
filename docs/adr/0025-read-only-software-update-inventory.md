# ADR 0025: Read-only software update inventory

Status: accepted

## Decision

Tako exposes installed-software updates as a read-only inventory. The adapter
selects an active PackageKit service first and uses bounded `pkcon` output when
available. When PackageKit is unavailable, version-gated APT and DNF simulation
queries are used; none of these paths installs packages or refreshes metadata.

Every response declares the selected backend and contract, bounds the package
list to 500 entries and command output to 4 MiB, and reports a package-manager
lock when a non-blocking lock probe detects one. Package fields include
installed/candidate versions, architecture, severity, size, and summary when
the selected backend provides them.

## Consequences

- A missing or unusable backend is explicit and fail-closed; Tako never turns a
  read-only inventory into package management.
- APT/DNF output is necessarily less rich than PackageKit metadata, so missing
  severity, size, or details are represented as unavailable fields.
- Package-manager locks are advisory runtime state and may change immediately
  after the bounded probe.
