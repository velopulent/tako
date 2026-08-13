# ADR 0011: Bounded service inspection and impact previews

Status: accepted

Service detail reads systemd through D-Bus. Relationship properties are decoded from object paths into unit names, while runtime properties and the fragment path remain read-only. A preview request rereads the current detail and returns a bounded list of affected relationships before the existing allowlisted lifecycle action can be confirmed.

Raw unit configuration is derived from the fragment path returned by systemd; clients never submit a file path. Tako resolves the path, requires the expected unit basename, restricts it to known systemd unit roots (including generated units) or user-unit roots, and reads at most 1 MiB. Symlinks resolving outside those roots are rejected. The API returns an explicit unavailable state when the current UNIX authority cannot read the file.
