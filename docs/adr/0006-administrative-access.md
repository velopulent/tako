# ADR 0006: Administrative access and allowlisted actions

Status: accepted

Tako sessions begin with normal UNIX-user privileges. The header exposes Administrative access, which requires reauthentication, expires after a configurable idle window, can be dropped explicitly, and is cleared with the web session. Passwords are bounded, forwarded only for authentication, cleared promptly, and never logged.

The network gateway remains unprivileged and cannot execute administrative commands. System service actions cross the authenticated local session boundary and are validated against fixed action, scope, and unit-name allowlists. The privileged session service records actor, unit, action, and result in journald. This narrow interface is intentionally not equivalent to a remote shell.

Administrative grants are issued by the privileged session service after it evaluates the invoking user's host policy. Sudo password and `NOPASSWD` policy are supported when configured; Polkit is an optional policy adapter. The grant is separate from the user-session token, short-lived, revoked on drop/logout/session loss, and required for system mutations. Interactive PAM/MFA remains governed by the host's configured policy and is not implemented in the network gateway.
