import { describe, expect, it } from "vitest"

import { pageTitle } from "@/lib/page-title"

describe("pageTitle", () => {
  it("shows one top-level page name for nested routes", () => {
    expect(pageTitle("/services/system/sshd.service")).toBe("Services")
    expect(pageTitle("/storage")).toBe("Storage")
    expect(pageTitle("/")).toBe("Dashboard")
  })

  it("falls back safely for an unknown route", () => {
    expect(pageTitle("/unknown")).toBe("Tako")
  })
})
