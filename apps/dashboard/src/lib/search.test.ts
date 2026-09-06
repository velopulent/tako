import { describe, expect, it } from "vitest"

import { networkSearch } from "./search"

describe("networkSearch", () => {
  it("omits the overview view while preserving search", () => {
    expect(networkSearch({ q: "eno1", view: "overview" })).toEqual({
      q: "eno1",
      view: undefined,
    })
  })

  it("keeps supported views in the URL search", () => {
    expect(networkSearch({ q: "ssh", view: "firewall" })).toEqual({
      q: "ssh",
      view: "firewall",
    })
  })

  it("falls back to overview for an invalid view", () => {
    expect(networkSearch({ q: "eno1", view: "unknown" })).toEqual({
      q: "eno1",
      view: undefined,
    })
  })
})
