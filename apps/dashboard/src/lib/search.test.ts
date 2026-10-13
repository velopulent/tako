import { describe, expect, it } from "vitest"

import { networkSearch } from "./search"

describe("networkSearch", () => {
  it("omits the default overview view", () => {
    expect(networkSearch({ view: "overview" })).toEqual({ view: undefined })
  })

  it("keeps supported views in the URL search", () => {
    expect(networkSearch({ view: "firewall" })).toEqual({ view: "firewall" })
    expect(networkSearch({ view: "logs" })).toEqual({ view: "logs" })
  })

  it("falls back to overview for removed or invalid views", () => {
    expect(networkSearch({ view: "configuration" })).toEqual({
      view: undefined,
    })
    expect(networkSearch({ view: "unknown" })).toEqual({ view: undefined })
  })
})
