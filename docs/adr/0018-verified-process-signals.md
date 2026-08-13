# ADR 0018: Verified process signals

## Context

Sending a signal by PID alone can affect a recycled process, and tree-wide
signals can include more work than an operator intended. The gateway must not
turn into a privileged kill service; UNIX ownership and explicit administrative
access remain authoritative.

## Decision

Tako exposes a read-only impact preview and a separate CSRF-protected apply
operation. Preview and apply identify every target by PID plus Linux starttime.
Apply rejects stale or changed target sets. The session service re-reads the
target set, enforces same-UID signaling for user bridge tokens, and permits
other-user targets only with a live administrative grant. It uses pidfd
signaling where available and falls back to a starttime-verified signal on
older kernels. Supported signals are a small allowlist; tree expansion is
capped at 1024 targets.

Every apply is logged with actor, signal, tree mode, target count, and result;
partial failures are returned explicitly. The dashboard requires impact
preview and typed `CONFIRM` for KILL or tree actions.

## Consequences

A process can exit between preview and apply, producing a visible conflict or
partial result rather than silently targeting a replacement. Unsupported
pidfd kernels retain a narrow verified fallback, and no arbitrary shell or
signal number is accepted.
