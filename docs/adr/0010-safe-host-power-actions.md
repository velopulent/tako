# ADR 0010: Safe host power actions

Status: accepted

Tako exposes only reboot and shutdown through the logind D-Bus contract. Read-only capability status includes the host's `CanReboot`/`CanPowerOff` result and active inhibitor details. The status is fingerprinted so an operator cannot confirm against stale policy or inhibitor state.

The browser must hold a live Administrative grant, send the current fingerprint, and type the exact action name (`REBOOT` or `SHUTDOWN`). The unprivileged gateway validates request shape and CSRF, while the privileged session boundary re-reads status, checks the fingerprint, validates the typed operation, and invokes only the fixed logind method. Arbitrary logind method names and arbitrary command arguments are never accepted. Power requests are acknowledged as potentially disconnecting the host and receive sanitized operation receipts.
