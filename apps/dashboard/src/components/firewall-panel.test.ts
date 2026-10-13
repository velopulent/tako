import { describe, expect, it } from "vitest"

import type { FirewallSnapshot } from "@/lib/api"
import { zonesOf } from "./firewall-panel"

const snapshot = (rules: string[], zones = ["public"]): FirewallSnapshot => ({
  backend: "firewalld",
  active: true,
  zones,
  rules,
  synchronized: true,
  conflicted: false,
  readOnly: false,
  fingerprint: "f",
})

describe("zonesOf", () => {
  it("groups firewalld list-all lines by zone", () => {
    const [zone] = zonesOf(
      snapshot([
        "public services: dhcpv6-client ssh",
        "public ports: 8080/tcp 53/udp",
        "public interfaces: eth0",
        "public target: default",
      ])
    )
    expect(zone).toMatchObject({
      name: "public",
      services: ["dhcpv6-client", "ssh"],
      ports: ["8080/tcp", "53/udp"],
      interfaces: ["eth0"],
      sources: [],
    })
  })

  it("keeps empty zones and rich rules", () => {
    const zones = zonesOf(
      snapshot(
        ['home rule family="ipv4" source address="10.0.0.0/8" accept'],
        ["public", "home"]
      )
    )
    expect(zones.map((zone) => zone.name)).toEqual(["public", "home"])
    expect(zones[1].rich).toHaveLength(1)
  })
})
