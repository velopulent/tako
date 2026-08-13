# Tako glossary

- **Gateway**: unprivileged HTTPS process serving UI and browser API.
- **Session service**: local privileged process limited to PAM authentication and user-session creation.
- **User bridge**: process running with logged-in UNIX user's identity and system permissions.
- **Privileged bridge**: short-lived bridge started only after system policy authorizes escalation.
- **Capability**: runtime statement that module dependency and permission are available.
- **Adapter**: module-owned implementation of a Linux or D-Bus system API.
- **Stream**: bounded, cancellable SSE or WebSocket data flow.
- **Module**: vertical UI, transport, domain behavior, and platform integration for one operator task.
- **Process mode**: one of the `tako` executable's isolated entry points: `serve`, `sessiond`, or `bridge`.
