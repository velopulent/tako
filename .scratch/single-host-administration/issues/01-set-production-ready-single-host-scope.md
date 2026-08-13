# Set production-ready single-host administration scope

Type: grilling
Status: resolved
Blocked by: None

## Question

What product boundary, platform target, safety posture, module scope, persistence policy, and delivery shape define this effort?

## Answer

- Target production-ready single-host administration matching common Cockpit outcomes, delivered in releasable stages rather than literal option parity.
- Support Ubuntu LTS, Debian stable, Fedora, and RHEL-compatible distributions; require systemd and expose missing optional integrations as capabilities.
- Operators authenticate with Linux credentials and act as their UNIX identity. Administrative access is time-bounded and used only for allowlisted privileged actions.
- Include production TLS/reverse-proxy/packaging work, Polkit, sudo, `NOPASSWD`, and interactive MFA. Defer localization, broad browser testing, and dedicated accessibility parity work.
- Add SQLite only for Tako-owned metadata: durable jobs, operation receipts, notification state, saved views, and server preferences. Linux remains authoritative for host state; journald remains security audit authority; credentials, PAM sessions, and elevation grants never enter SQLite.
- Include host controls, deeper services/logs/processes, users, installed-software updates, Files with Media preview, networking, firewalls, SELinux, AppArmor, troubleshooting, diagnostic reports, notifications, and operations history.
- Keep current read-only storage monitoring, but defer storage administration, containers, VMs, fleet management, extensions, MCP, and resource benchmarking.
- Use NetworkManager and firewalld first while including UFW and designing module-owned adapters for later backends.
- Mutations use preview, exact authority, stale-write detection, reauthentication where needed, confirmation proportional to risk, audit, cancellation, and recovery guidance. Network edits automatically roll back unless connectivity is confirmed.
- Files support browsing, create, rename, move, copy, paste, upload, download, trash/delete, editing, permissions, ownership, archives, hidden files, bounded search, and deliberate symlink behavior. File I/O runs only in user or short-lived privileged bridges.
- Media preview supports browser-native images, video, audio, PDF, bounded text/code, and JSON. Media uses authenticated byte ranges; active content is sandboxed or refused; unknown files offer metadata and download.
- Storage and network pages show only relevant metrics: separate storage read/write and network receive/transmit charts, with aggregate and selectable device/interface history. Site header shows current page identity; duplicate page breadcrumbs are removed, except Files path navigation.
- Produce one umbrella program spec and ten focused module specs. Implement foundations first, then UI corrections, host/services, logs/processes, users, updates, Files, network/firewall, security, and troubleshooting/notifications.

## Comments

- Resolved by product interview before map creation.
