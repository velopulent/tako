# ADR 0003: System integrations and persistence

Status: accepted

Tako uses systemd, journald, PackageKit, UDisks2, and NetworkManager over D-Bus. Missing optional services disable only their module and produce setup guidance. Shell-command parsing is not a primary integration contract.

Tako owns no database in v1. Linux and D-Bus services remain authoritative. Configuration is strict TOML, sessions and recent metrics are memory-only, and audit records go to journald.

