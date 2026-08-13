# ADR 0023: Safe authorized SSH key management

## Context

Authorized SSH keys are access-control state, not arbitrary text files. A
web-facing editor must not follow a symlink, overwrite an unexpected path,
silently discard a concurrent change, or leave a partially written file.
Remote NSS identities also do not provide a local filesystem authority.

## Decision

Tako targets only local `/etc/passwd` accounts and resolves the account home
from NSS. It rejects a symlink or non-directory home `.ssh` component and a
symlink or non-regular `authorized_keys` file. The session service is the only
process that reads or writes the path. It bounds file size, line length, key
count, and key blob size; accepts supported OpenSSH public-key types while
rejecting `ssh-dss`; and identifies each key by a SHA-256 fingerprint of the
decoded key blob.

Every add/remove operation carries the authorized-keys file fingerprint from
preview. Apply writes a temporary file in the same `.ssh` directory with mode
`0600`, sets the target UID/GID, syncs it, renames it atomically, and syncs the
directory. The directory is mode `0700`. Comments and blank lines are retained.
Stale fingerprints conflict, and post-write key counts are verified.

The gateway authorizes the signed-in user's own key path with the user bridge;
another account requires the short-lived Administrative grant. Receipts record
only `user/<name>/ssh-keys/<action>`, never the public key line or comments.

## Consequences

Operators get explicit path, authority, and stale-write information while
protected path anomalies fail closed. Hosts with remote-only identities or
unusual authorized-key formats remain readable only when the file can be
validated; unsupported content is rejected rather than rewritten. The unit,
HTTP, dashboard, and Linux filesystem tests cover atomic permissions, stale
writes, comments, symlink protection, authorization, and safe receipts.
