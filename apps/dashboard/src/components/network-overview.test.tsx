import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { NetworkResponse } from "@/lib/api"
import { State } from "@/lib/page"
import { NetworkDetails, NetworkSummary } from "./network-overview"

const snapshot: NetworkResponse = {
  items: [
    {
      name: "eno1",
      index: 2,
      mtu: 1500,
      hardware: "00:11:22:33:44:55",
      addresses: ["192.0.2.10/24"],
      up: true,
      rx: 10,
      tx: 20,
      manager: "NetworkManager",
      owner: "NetworkManager",
    },
  ],
  addresses: [
    {
      interface: "eno1",
      address: "192.0.2.10/24",
      family: "inet",
      scope: "global",
    },
    {
      interface: "eno1",
      address: "2001:db8::10/64",
      family: "inet6",
      scope: "global",
    },
  ],
  routes: [
    {
      destination: "default",
      gateway: "192.0.2.1",
      device: "eno1",
      metric: 100,
    },
  ],
  dns: ["192.0.2.53"],
  ownership: {
    activeOwner: "NetworkManager",
    detected: ["NetworkManager"],
    conflicted: true,
    reason: "NetworkManager and netplan both reported ownership.",
  },
}

describe("network overview", () => {
  it("renders addresses, routes, DNS, ownership, and conflict state", () => {
    render(
      <>
        <NetworkSummary snapshot={snapshot} />
        <NetworkDetails snapshot={snapshot} />
      </>
    )

    expect(screen.getByText("NetworkManager")).toBeTruthy()
    expect(screen.getByText("1/1 up")).toBeTruthy()
    expect(screen.getByText("1 IPv4 · 1 IPv6")).toBeTruthy()
    expect(screen.getByText("192.0.2.10/24")).toBeTruthy()
    expect(screen.getAllByText("global")).toHaveLength(2)
    expect(screen.getByText("default")).toBeTruthy()
    expect(screen.getByText("192.0.2.53")).toBeTruthy()
    expect(screen.getByText("Ownership conflict")).toBeTruthy()
    expect(
      screen.getByText("NetworkManager and netplan both reported ownership.")
    ).toBeTruthy()
  })

  it("keeps empty detail states explicit", () => {
    render(
      <NetworkDetails
        snapshot={{
          items: [],
          addresses: [],
          routes: [],
          dns: [],
        }}
      />
    )

    expect(screen.getByText("No addresses reported.")).toBeTruthy()
    expect(screen.getByText("No routes reported.")).toBeTruthy()
    expect(screen.getByText("No DNS servers reported.")).toBeTruthy()
  })

  it("renders the shared error state used by the network tabs", () => {
    render(
      <State query={{ isPending: false, isError: true }} empty={false}>
        <span>unreachable</span>
      </State>
    )

    expect(screen.getByText("Could not load data")).toBeTruthy()
    expect(screen.queryByText("unreachable")).toBeNull()
  })
})
