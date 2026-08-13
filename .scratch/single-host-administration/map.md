# Wayfinder map: Production-ready single-host administration

## Destination

Produce an agreed suite of implementation-ready specs for a production-ready, single-host Tako that covers common Cockpit administration outcomes through staged, releasable milestones without cloning every Cockpit option.

## Notes

- Use `grill-with-docs` and domain modeling while resolving decisions.
- Preserve Tako's gateway, session service, user bridge, and privileged bridge trust boundaries.
- Prefer stable Linux APIs. Command adapters are allowed only when they invoke fixed executables with argument arrays, strict parsers, timeouts, cancellation, capability detection, and allowlisted mutations.
- Plan one umbrella program spec plus module specs for platform foundations; host and services; logs, processes, and troubleshooting; users; updates; Files; network and firewall; SELinux and AppArmor; notifications and operations; and existing UI corrections.
- Specs must preserve resource-sensitive streaming and bounded-memory behavior.
- Implementation remains outside this map. After decisions close: `to-spec`, then `to-tickets`, then `implement`.

## Decisions so far

- [Set production-ready single-host administration scope](issues/01-set-production-ready-single-host-scope.md) — Defined supported platforms, included modules, safety posture, persistence boundary, delivery shape, and explicit deferrals.

## Not yet specified

- Shared architecture for SQLite-backed jobs, operation receipts, notifications, saved views, preferences, capability reporting, optimistic concurrency, cancellation, and progress streaming.
- Final navigation and interaction system for page titles, relevant metric charts, action surfaces, Administrative access, dangerous-operation previews, Files, and Media preview.
- Module boundaries and contracts for host controls and systemd services.
- Journal query, process inspection, troubleshooting correlation, and diagnostic-report contracts.
- User, group, SSH-key, administrative-role, password, and login-history workflows.
- Update transaction model across PackageKit and native APT/DNF adapters.
- Files bridge protocol, filesystem operation semantics, resumable transfers, large-text viewing, and secure Media preview.
- Network adapter model and rollback protocol across NetworkManager, firewalld, and UFW, with later adapters kept easy to add.
- SELinux and AppArmor inspection and narrowly-scoped remediation contracts.
- Notification conditions, acknowledgement lifecycle, operation history, retention, and certificate monitoring.
- Packaging, TLS, reverse-proxy, PAM, Polkit, sudo, `NOPASSWD`, interactive MFA, and supported-distribution release verification.
- Unit, handler, UI, and privileged VM integration-test seams.

## Out of scope

- Fleet and multi-host management.
- Extension or third-party module SDK.
- MCP support.
- Storage administration: disks, partitions, formatting, mounts, LVM, RAID, LUKS, SMART, NFS, and iSCSI. Existing read-only capacity monitoring remains.
- Podman, Docker, Kubernetes, and other container administration.
- Libvirt and virtual-machine administration.
- Resource comparison or benchmark targets against Cockpit.
- Dedicated accessibility parity program, localization, RTL, and broad browser matrix. Existing accessible shadcn behavior must not be removed.
- Kerberos SSO and client-certificate authentication.
- General package search, installation, removal, and repository management; Terminal remains escape hatch.
- General notification expression language, email, and webhooks.
- Full-content filesystem indexing, media transcoding, and permanent thumbnail infrastructure.
- Exact option-for-option Cockpit parity.
