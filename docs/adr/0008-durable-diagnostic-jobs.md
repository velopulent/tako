# ADR 0008: Durable diagnostic jobs

Status: accepted

Read-only diagnostics use the shared SQLite metadata store as a durable job ledger. A job is identified by an opaque id and moves through `pending`, `running`, `succeeded`, `failed`, `canceled`, or `interrupted`. Progress and a short operator-facing message are persisted after each bounded stage, so the dashboard can reconnect after navigation or a temporary network failure.

The gateway owns a bounded queue and two workers. Each job has a finite execution context; cancellation marks the durable row immediately and cancels the active worker context. Startup recovery marks pending and running rows as `interrupted` rather than retrying them. This rule is safe for future diagnostic jobs that may include higher-impact work: interruption is visible and recovery remains deliberate.

The initial job is host inventory. Its report contains host identity and sanitized runtime capability metadata. It never stores credentials, session or Administrative grants, raw command output, or journal copies. The HTTP API exposes list/detail/start/cancel operations behind the authenticated router and CSRF protection for writes.
