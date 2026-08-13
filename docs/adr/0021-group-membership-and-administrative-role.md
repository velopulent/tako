# ADR 0021: Structured group and administrator-role management

## Context

UNIX group membership is the authority for many host permissions, but remote
NSS groups must not be treated as local databases. Free-form sudoers editing
would turn the web gateway into an unbounded privilege escalation surface.

## Decision

Tako exposes typed add/remove membership operations and a separate
`administrator` role operation. Membership changes target only a selected
local group and local user, invoke fixed `gpasswd --add/--delete` arguments in
sessiond, and verify the resulting NSS membership. Every apply carries a
group fingerprint obtained during preview; stale fingerprints conflict.

The administrator role resolves to an existing local `sudo` or `wheel` group
using deterministic selection, and uses the same membership adapter. Grant and
revoke require explicit typed confirmation. The current operator and the last
member of the selected administrator group cannot be revoked. No endpoint
accepts sudoers text, arbitrary group commands, or remote identity writes.

## Consequences

Role changes remain reviewable and auditable while retaining distro-specific
UNIX authority. Hosts without a local `sudo` or `wheel` group report the role
as unavailable instead of inventing policy. A group provider or NSS cache that
changes during an operation causes a visible conflict or verification failure.
