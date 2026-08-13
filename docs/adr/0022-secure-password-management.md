# ADR 0022: Secure password management through PAM

## Context

Tako needs self-service password changes and administrative resets without
turning the gateway, SQLite operation history, or process table into a secret
store. Calling `passwd` or another helper with a password in argv, an
environment variable, a job, or a receipt would expose credentials to other
local observers.

## Decision

The gateway accepts one bounded, strict `PasswordChangeOperation` request. A
`change` operation is bound to the signed-in user's session bridge; a `reset`
operation requires the short-lived administrative grant and names the target
account. The gateway forwards the operation over the existing protected UNIX
socket and records only a non-secret target such as
`user/alice/password-change`.

Sessiond validates the operation and calls the standard `passwd` PAM service
through an in-memory conversation. The current password is used only for the
self-service authentication phase; a reset supplies only the replacement
password to PAM. PAM errors are reduced to safe authentication, expired,
policy, invalid, or unavailable codes. No password is placed in argv, stdin,
the environment, SQLite, jobs, receipts, logs, or an API response. Request
and operation buffers are cleared as soon as the PAM call returns.

## Consequences

Password policy, aging, history, and account-specific rules remain owned by
the host PAM stack rather than duplicated in Tako. Hosts without a usable
`passwd` PAM service fail closed with a safe availability error. The opt-in
Linux integration test changes and restores a disposable VM fixture account;
unit and UI tests use a recording backend and never require real credentials.
