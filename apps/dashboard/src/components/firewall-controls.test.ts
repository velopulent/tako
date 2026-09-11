import { describe, expect, it } from "vitest"

import { groupFirewallRules } from "./firewall-controls"

describe("groupFirewallRules", () => {
  it("groups firewalld output by zone and keeps rule details", () => {
    expect(
      groupFirewallRules([
        "public services: ssh cockpit",
        "public ports: 443/tcp",
        "libvirt ports: 53/udp",
      ])
    ).toEqual([
      {
        zone: "public",
        rules: ["services: ssh cockpit", "ports: 443/tcp"],
      },
      { zone: "libvirt", rules: ["ports: 53/udp"] },
    ])
  })

  it("uses a raw-rule fallback for unparsed backend output", () => {
    expect(
      groupFirewallRules(["22/tcp ALLOW Anywhere", "80/tcp ALLOW Anywhere"])
    ).toEqual([
      {
        zone: "Unzoned rules",
        rules: ["22/tcp ALLOW Anywhere", "80/tcp ALLOW Anywhere"],
      },
    ])
  })
})
