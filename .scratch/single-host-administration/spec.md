Status: ready-for-agent

# Production-ready single-host administration

## Problem Statement

Tako gives an operator a useful single-host dashboard, live metrics, process and journal inspection, a web terminal, local-user authentication, mounted-filesystem capacity, network inventory, and systemd lifecycle actions. Most administration areas remain observational, however. Users still need a terminal for routine host configuration, account maintenance, updates, file operations, network and firewall changes, security-policy investigation, and recovery from common failures.

This prevents Tako from serving as a credible modern alternative to Cockpit for common single-server administration. Closing that gap requires more than adding controls. Tako must preserve UNIX identity, its isolated process modes, bounded resource use, safe remote mutation, cross-distribution behavior, durable long-running work, and an audit trail while exposing dangerous Linux operations through a browser.

## Solution

Expand Tako into a production-ready, single-host administration console for Ubuntu LTS, Debian stable, Fedora, and RHEL-compatible systems. Keep Linux services authoritative for host state, use stable system APIs where available, and isolate all filesystem and privileged work from the network-facing Gateway.

Complete common workflows for host controls, systemd services, logs, processes, users, installed-software updates, Files with secure Media preview, networking, firewalls, SELinux, AppArmor, troubleshooting, notifications, and operation history. Add SQLite only for Tako-owned durable metadata such as jobs, operation receipts, notifications, saved views, and server-side preferences. Deliver the work as staged, releasable vertical slices with proportional safety controls and real-system integration coverage.

## User Stories

1. As an operator, I want Tako to show hostname, operating system, kernel, architecture, uptime, hardware, boot history, and restart-required state, so that I can understand the host from one overview.
2. As an administrator, I want to change hostname, timezone, and NTP configuration, so that I can perform routine host setup without a terminal.
3. As an administrator, I want guarded reboot and shutdown actions, so that power operations are deliberate and auditable.
4. As an operator, I want the header to show only the current page identity, so that navigation is clear without duplicate breadcrumbs.
5. As an operator, I want the Storage page to show capacity and storage-only telemetry, so that unrelated CPU and memory charts do not distract me.
6. As an operator, I want separate storage read and write charts, so that I can distinguish I/O direction.
7. As an operator, I want aggregate and selectable per-device storage history, so that I can find a busy device.
8. As an operator, I want the Network page to show network-only telemetry, so that it reflects its task.
9. As an operator, I want separate receive and transmit charts, so that I can identify traffic direction.
10. As an operator, I want aggregate and selectable per-interface network history, so that I can isolate an interface problem.
11. As an operator, I want to inspect system and user services, sockets, timers, targets, and paths, so that I can understand systemd state.
12. As an administrator, I want to start, stop, restart, reload, enable, disable, mask, and unmask allowed units, so that I can control services safely.
13. As an administrator, I want to create and edit structured systemd timers, so that scheduled work does not require hand-written unit files.
14. As an administrator, I want to manage safe systemd overrides and environment settings, so that I can customize services without replacing vendor units.
15. As an operator, I want raw unit files to be readable, so that I can inspect exact configuration.
16. As an operator, I want dependency impact shown before disruptive unit actions, so that I understand affected services.
17. As an administrator, I want daemon reload performed only when required and confirmed, so that systemd sees intended changes.
18. As an operator, I want service details correlated with recent logs and resource use, so that diagnosis is faster.
19. As an operator, I want journal filtering by boot, time, priority, unit, executable, and text, so that I can narrow incidents precisely.
20. As an operator, I want cursor-based journal pagination, so that large journals remain bounded and navigable.
21. As an operator, I want live journal following that does not force-scroll after I move away from latest entries, so that incoming data does not disrupt investigation.
22. As an operator, I want detailed journal fields on demand, so that routine views remain readable while forensic data stays available.
23. As an operator, I want to export filtered logs, so that I can share evidence with support teams.
24. As an operator, I want saved journal views, so that repeated investigations need less setup.
25. As an operator, I want to inspect process trees, cgroups, open files, sockets, and resource history, so that I can identify misbehaving workloads.
26. As an operator, I want to send signals to my own processes, so that I can control them under my UNIX authority.
27. As an administrator, I want to signal processes owned by other users after gaining Administrative access, so that privileged control remains explicit.
28. As an administrator, I want a clear impact preview before signaling a process tree, so that I do not accidentally terminate unrelated work.
29. As an operator, I want local users and groups displayed with roles, status, SSH keys, and login history, so that account state is understandable.
30. As an administrator, I want to create, edit, lock, unlock, and delete local accounts, so that I can manage access without a terminal.
31. As an administrator, I want to manage group membership, so that UNIX authorization remains authoritative.
32. As an administrator, I want to set or reset account passwords through bounded secret handling, so that passwords are never stored or logged by Tako.
33. As an operator, I want to manage my authorized SSH keys, so that remote access can be maintained safely.
34. As an administrator, I want to manage another user's authorized SSH keys with Administrative access, so that access recovery is possible.
35. As an administrator, I want a defined administrative-role control instead of a free-form sudoers editor, so that privilege changes stay structured and auditable.
36. As an operator, I want to see available installed-software updates with severity, version, size, and changelog information, so that I can judge urgency and impact.
37. As an administrator, I want to apply selected or all updates, so that maintenance matches my risk tolerance.
38. As an administrator, I want update progress to survive navigation and reconnection, so that long transactions remain observable.
39. As an administrator, I want failed updates to report actionable recovery guidance, so that partial failures are understandable.
40. As an operator, I want Tako to identify services needing restart and whether the host needs reboot, so that updates can be completed correctly.
41. As an administrator, I want to configure supported automatic security-update policy later without blocking current manual workflows, so that rollout remains staged.
42. As an operator, I want to browse files and directories available to my UNIX identity, so that filesystem access matches terminal access.
43. As an operator, I want inaccessible directories displayed as locked without leaking their children, so that permissions are visible without metadata disclosure.
44. As an operator, I want to create files and directories, rename, move, copy, and paste items, so that common file work is graphical.
45. As an operator, I want context-menu, toolbar, row-menu, touch, and keyboard action paths, so that file operations are discoverable across devices.
46. As an operator, I want streamed downloads, so that large files do not consume Gateway memory.
47. As an operator, I want bounded resumable uploads with disk-space preflight and checksum validation, so that large transfers survive unreliable connections.
48. As an operator, I want supported deletions to use trash first, so that accidental removal can be undone.
49. As an administrator, I want impact summaries and typed confirmation for recursive, privileged, or permanent deletion, so that destructive actions are deliberate.
50. As an operator, I want to view and change file permissions and ownership within my authority, so that filesystem access can be maintained.
51. As an operator, I want to create and extract common archives with path-safety checks, so that bundled files are manageable.
52. As an operator, I want hidden-file visibility as an explicit preference, so that normal browsing remains uncluttered.
53. As an operator, I want bounded directory search without full-content indexing, so that I can find names without building a resource-heavy index.
54. As an operator, I want symlinks displayed distinctly with target metadata, so that links are not mistaken for regular files.
55. As an operator, I want link operations to affect the link by default rather than its target, so that copy and delete behavior is safe.
56. As an operator, I want to edit detected text files up to a configurable 5 MiB limit with atomic saves, so that routine configuration editing is safe.
57. As an operator, I want stale file writes rejected with a conflict view, so that external edits are not silently overwritten.
58. As an operator, I want oversized text files shown through a server-windowed, virtualized, read-only viewer, so that browser memory stays bounded.
59. As an operator, I want images previewed using browser-native rendering, so that I can inspect media without downloading it first.
60. As an operator, I want audio and video preview with HTTP Range seeking, so that large media does not need full download.
61. As an operator, I want PDFs opened in a sandboxed browser-native viewer, so that documents remain convenient without sharing Tako's trusted page context.
62. As an operator, I want JSON and text/code routed to the text viewer or editor according to size, so that structured text has one consistent experience.
63. As an operator, I want unknown or binary files to show metadata and download/open options, so that Tako does not unsafe-preview unknown content.
64. As an operator, I want preview and download URLs to be session-bound, short-lived, and tied to one file version, so that URLs cannot become durable access tokens.
65. As an operator, I want to inspect interface state, addresses, routes, DNS, link details, and the detected manager, so that networking is understandable.
66. As an administrator, I want to configure DHCP and static addresses, DNS, routes, bonds, bridges, VLANs, and supported Wi-Fi, so that common network configuration is graphical.
67. As an administrator, I want NetworkManager-backed changes first with an adapter boundary for later systemd-networkd and Netplan support, so that support grows without rewriting domain behavior.
68. As an operator, I want to inspect firewall state and active rules through firewalld or UFW, so that exposure is visible on supported hosts.
69. As an administrator, I want to add, edit, enable, disable, and remove supported firewall services, ports, sources, and rules, so that common firewall work avoids the terminal.
70. As an administrator, I want warnings when firewall edits may block SSH or Tako, so that remote access is protected.
71. As an administrator, I want network changes to roll back automatically unless restored connectivity is confirmed, so that mistakes do not permanently lock me out.
72. As an operator, I want unsupported network or firewall backends reported clearly as capabilities, so that read-only fallback is not mistaken for successful management.
73. As an operator, I want SELinux or AppArmor status and recent denials shown according to the active host framework, so that policy failures are discoverable.
74. As an operator, I want security denials explained with relevant process, path, profile/domain, and policy context, so that I can understand likely cause.
75. As an administrator, I want to change known SELinux booleans and supported AppArmor profile states after preview, so that common remediations are bounded.
76. As an administrator, I want narrowly generated policy overrides previewed before application, so that fixes do not silently grant broad access.
77. As an administrator, I want Tako never to suggest disabling enforcement as routine remediation, so that security posture is preserved.
78. As an operator, I want one incident workspace correlating metrics, failed services, processes, logs, updates, security denials, and recent Tako actions, so that evidence is collected in one timeline.
79. As an operator, I want downloadable diagnostic reports with an explicit secret-redaction warning, so that I can share troubleshooting data responsibly.
80. As an operator, I want actionable in-app notifications for disk pressure, failed services, required restart/reboot, security denials, network degradation, and configured certificate expiry, so that important conditions are visible.
81. As an operator, I want to acknowledge, mute, and observe resolution of notifications, so that notification state reflects my workflow.
82. As an administrator, I want built-in notification thresholds configurable without a general rule language, so that configuration remains understandable.
83. As an operator, I want operation history showing actor, target, preview, time, result, failure, and Administrative access use, so that Tako changes are traceable.
84. As an operator, I want long operations represented as durable jobs with progress, cancellation, and reconnect support, so that navigation does not lose work.
85. As an administrator, I want dangerous interrupted jobs never to retry silently, so that recovery remains deliberate.
86. As an operator, I want external changes detected before saving Files, users, network, timers, or settings, so that Tako does not overwrite newer host state.
87. As an operator, I want capabilities to explain missing integrations and setup guidance, so that partial host support is transparent.
88. As a Linux user, I want to authenticate through PAM and act under my UNIX identity, so that Tako does not invent another account system.
89. As an administrator, I want Polkit, sudo, `NOPASSWD`, and interactive MFA supported through isolated bridges, so that distribution policy works without privileging the Gateway.
90. As an administrator, I want Administrative access to remain time-bounded, explicitly droppable, and cleared with my session, so that elevation cannot persist accidentally.
91. As a security reviewer, I want every privileged action validated against typed operation and argument allowlists, so that no generic remote command channel exists.
92. As a security reviewer, I want credentials and secrets bounded, promptly cleared, and excluded from SQLite, logs, receipts, and responses, so that Tako does not retain authentication material.
93. As a deployer, I want supported packages, systemd units, TLS configuration, and reverse-proxy guidance, so that Tako can be installed safely on headless servers.
94. As a maintainer, I want real VM integration tests across supported distribution families, so that privileged behavior is verified against actual system services.
95. As a maintainer, I want every release stage independently usable and testable, so that this program does not require one risky all-at-once launch.

## Implementation Decisions

- Tako remains a single-host Linux administration application. Fleet management, MCP, and an extension SDK do not enter this program.
- Supported first-class platform families are Ubuntu LTS, Debian stable, Fedora, and RHEL-compatible distributions. systemd is required; optional integrations are detected at runtime.
- Preserve existing process isolation: Gateway handles network parsing and browser APIs; Session service handles PAM and user-session creation; User bridge acts as the authenticated UNIX identity; Privileged bridge performs short-lived allowlisted operations after policy authorization. No filesystem or privileged host work moves into Gateway.
- Linux and its services remain authoritative for host state. Prefer D-Bus and documented system APIs. Where unavailable, a module-owned command adapter may invoke only fixed executables with argument arrays, strict parsers, bounded input/output, deadlines, cancellation, capability checks, and explicit operation allowlists. Shell strings and generic command execution are forbidden.
- Add one migrated SQLite database under Tako's data directory, using WAL mode, for Tako-owned durable metadata only: jobs, operation receipts, notification state, saved views, and server preferences. Credentials, secrets, PAM sessions, Administrative access grants, journal copies, and replicated Linux configuration are forbidden.
- Journald remains the security audit authority. SQLite operation receipts support product history and recovery but do not replace the audit log.
- Long-running work uses a shared job model with explicit states, progress, cancellation rules, reconnectable observation, sanitized failure details, and no silent retry of dangerous mutations.
- Mutations use proportional safety controls: current-state fingerprints, preview where meaningful, exact authority checks, reauthentication where needed, explicit confirmation, typed confirmation for high-impact actions, bounded execution, result verification, operation receipts, and recovery guidance.
- Stale writes to Files, users, network configuration, timers, and settings are rejected. The user may refresh or deliberately overwrite when policy allows.
- Host controls include hostname, timezone, NTP, reboot, shutdown, hardware inventory, boot history, and restart-required state.
- systemd support includes system/user units, lifecycle operations, structured timer creation/editing, safe drop-in overrides and environment settings, daemon reload, read-only raw unit files, dependency impact, and correlated logs. Tako does not provide an unrestricted raw system-unit editor.
- Journal support uses bounded cursor pagination and live SSE following. It supports boot, time, priority, unit, executable, and text filters; detailed fields; export; saved views; and correlation. Journal content is not copied into SQLite.
- Process support includes trees, cgroups, open files, sockets, resource history, and signals. UNIX permissions govern same-user actions; other-user actions require Administrative access. Tree-wide actions require impact confirmation.
- User management covers local accounts, groups, passwords, SSH keys, locking, defined administrative-role membership, and login history. Free-form sudoers editing is excluded.
- Update behavior is exposed through one stable domain API with PackageKit first and native APT/DNF adapters where PackageKit cannot provide required behavior. Scope is installed-software updates and supported update policy, not general package or repository management.
- Files exposes create, rename, move, copy/paste, upload, download, trash/delete, permissions, ownership, archives, hidden files, bounded name search, and explicit symlink semantics. Filesystem work runs in User bridge or a short-lived Privileged bridge. Directory listings never reveal children without authority.
- Downloads stream. Uploads are chunked and resumable with configured limits, disk-space checks, checksums, cancellation, and explicit recovery. Gateway never buffers whole large files.
- Text editing is limited by default to detected text up to 5 MiB. Saves are atomic and fingerprinted. Oversized text uses a server-windowed, virtualized, read-only viewer; TanStack Virtual may bound rendered rows but must not be treated as a network or memory bound by itself.
- `MediaPreview` dispatches by trusted server classification: browser-native image, video, audio, sandboxed PDF, bounded text/code/JSON, or metadata plus download. No initial transcoding or durable thumbnail system exists.
- Media content supports authenticated conditional and HTTP Range requests, including `206 Partial Content`, `Accept-Ranges`, and `Content-Range`, without bypassing per-request authorization. Preview endpoints set explicit content types, `nosniff`, restrictive content disposition, and sandbox/isolation appropriate to active content. HTML is never rendered inline; SVG is isolated or downloaded.
- Preview/download grants are short-lived, session-bound, exact-file, exact-version capabilities. They are not durable or generally shareable.
- Symlinks are displayed as links. Explicit navigation may follow a freshly authorized target; copy/delete acts on the link by default. Filesystem operations must resist traversal, archive extraction attacks, and symlink races through descriptor-relative techniques where available.
- Network configuration uses NetworkManager first while keeping adapters open for systemd-networkd and Netplan. Firewall support includes firewalld and UFW in this program. Unsupported backends report capability reasons and never pretend a mutation succeeded.
- Network mutations use checkpoints and automatic rollback unless the browser reconnects and confirms restored connectivity before the deadline. Tako and SSH exposure changes receive specific warnings.
- SELinux and AppArmor share a security-policy experience while retaining framework-specific adapters. Tako may inspect and explain denials, change known booleans/profile state, and apply narrowly scoped previewed overrides. It never silently generates broad allow policy or routinely recommends disabling enforcement.
- Troubleshooting provides a correlation workspace and diagnostic-report jobs without any AI dependency. Report generation warns about possible secrets and applies bounded redaction.
- Notifications use built-in condition types with configurable thresholds, mute, acknowledge, and resolved states. Initial delivery is in-app. Certificate monitoring covers Tako's configured certificate and explicitly configured paths only.
- Storage administration, containers, and virtual machines are deferred. Existing mounted-filesystem capacity remains. Storage and Network pages receive relevant-only telemetry and separate directional charts, with optional per-device/interface selection.
- Site header shows current page identity rather than a full breadcrumb trail. Page-level duplicate breadcrumbs are removed. Files retains path navigation because it changes browsing location.
- UI uses existing shadcn/Base UI primitives first. File actions use Context Menu as a shortcut plus visible toolbar/row actions for touch and discoverability. Existing keyboard, mobile, dark-mode, loading, error, and empty-state conventions remain required.
- Delivery order is platform foundations; existing UI corrections; host and services; logs and processes; users; updates; Files; network and firewall; SELinux and AppArmor; troubleshooting, notifications, and operations. Every stage must remain releasable.

## Testing Decisions

- Tests assert externally visible behavior, authorization boundaries, persistence, protocol framing, and system outcomes rather than private function calls or adapter internals.
- Primary seam is the authenticated HTTP API through the real router with a temporary SQLite database and controlled platform/bridge adapters. This seam covers session authority, CSRF, request limits, validation, capability behavior, job state, cancellation, conflicts, previews, Range/conditional responses, structured errors, and operation receipts.
- UI seam covers complete route/component workflows: loading, error, empty, keyboard, mobile, dark mode, selection, confirmations, progress, reconnect, stale conflicts, locked Files, Media preview dispatch, relevant-only charts, and current-page titles.
- System seam uses privileged disposable VMs across supported distribution families for PAM, Polkit, sudo, `NOPASSWD`, MFA, systemd, PackageKit/APT/DNF, NetworkManager, firewalld, UFW, network rollback, SELinux, AppArmor, filesystem authority, HTTP Range serving, diagnostics, and packaging behavior.
- Release seam installs built packages in clean VMs, starts socket/service units, authenticates, elevates, completes representative read/write workflows, restarts Tako, and verifies durable jobs/receipts plus absence of persisted secrets.
- Existing Go test patterns for handlers, parsers, bounded streams, session expiry, authorization, bridge framing, cancellation, and race testing remain prior art.
- Existing dashboard type checking remains mandatory. Add behavior-focused UI tests at route/component seams rather than snapshot-heavy tests.
- API behavior changes update the OpenAPI contract and include handler coverage.
- Security-sensitive code runs Go race tests plus malformed-input, path-traversal, symlink-race, archive traversal, command-argument injection, output-bound, timeout, disconnect, and cancellation cases.
- File-serving tests cover valid, suffix, open-ended, invalid, unsatisfiable, conditional, and `If-Range` requests; authorization is rechecked for every request.
- Network tests prove rollback after lost browser connectivity and commit after explicit reconnection confirmation.
- Persistence tests prove migrations, restart recovery, retention, WAL behavior, cancellation state, sanitized failures, and exclusion of secrets.
- Each vertical implementation slice carries its own tests; full workspace test, lint, typecheck, race, and VM release suites run at appropriate gates.

## Out of Scope

- Fleet and multi-host management.
- Extension or third-party module SDK.
- MCP support.
- Storage administration: disks, partitions, formatting, mounts, LVM, RAID, LUKS, SMART, NFS, and iSCSI. Existing read-only capacity monitoring remains.
- Podman, Docker, Kubernetes, Compose, and other container administration.
- Libvirt, remote hypervisors, and virtual-machine administration.
- Resource comparison or benchmark targets against Cockpit.
- Dedicated accessibility parity initiative, localization, RTL, and broad browser matrix. Existing accessible shadcn behavior must not regress.
- Kerberos SSO and client-certificate authentication.
- General package search, installation, removal, and repository management.
- General notification rule language, email, webhook, and external incident-system delivery.
- Full-content filesystem indexing, media transcoding, and permanent thumbnail infrastructure.
- Arbitrary shell execution, arbitrary privileged commands, unrestricted system-unit editing, and free-form sudoers editing.
- Exact option-for-option Cockpit parity.

## Further Notes

- This is the umbrella program spec. Focused module specs should preserve these decisions and provide smaller contexts for ticket generation.
- Research decision tickets remain the source for exact cross-distribution adapter choices and authentication/policy portability. Their findings may refine implementation details without expanding product scope.
- “Better than Cockpit” means safer mutations, stronger troubleshooting correlation, polished Files/Media workflows, modern coherent UI, and a smaller trust surface—not a claim of broader feature count.
- Resource-sensitive implementation remains a standing design requirement even though competitive resource benchmarking is deferred.
