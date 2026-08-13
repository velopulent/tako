# Administrative policy verification matrix

Tako evaluates elevation in the root-owned session service. The gateway never
re-authenticates or runs `sudo`/Polkit itself. Each release VM should exercise:

| Host policy | Expected path | Acceptance |
| --- | --- | --- |
| `NOPASSWD: /usr/bin/true` | sudo non-interactive | short-lived grant, no password retained |
| password-backed sudo | `sudo -S` under the authenticated UID | wrong password denied; bounded stderr |
| Polkit `org.velopulent.tako.administrate` | `pkcheck --allow-user-interaction` | policy result is authoritative |
| no matching policy | unavailable/denied | no grant and no privileged mutation |

The VM checks expiration, explicit drop, logout, bridge loss, and session loss.
MFA stacks remain host-owned: the browser clears each submitted secret and lets
the operator retry the current PAM challenge without persisting responses.
