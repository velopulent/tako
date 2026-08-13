# ADR 0009: Host identity and time configuration

Status: accepted

Hostname, timezone, and NTP are managed through the system D-Bus `hostname1` and `timedate1` contracts. The network gateway only validates and previews a requested change. The privileged session boundary re-reads current state, compares the caller's fingerprint, and performs the typed operation. This prevents a stale browser from overwriting another operator's change.

Every successful mutation is followed by a fresh read and exact result verification. Failures are surfaced as unavailable, denied/expired, stale conflict, or verification failure; the raw D-Bus error is not returned to the browser. Successful and failed attempts receive sanitized SQLite operation receipts. No host configuration, credentials, or Administrative token is stored in the durable metadata database.
