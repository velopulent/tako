import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { UserInventory } from "@/components/user-inventory"

function renderInventory() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <UserInventory />
    </QueryClientProvider>
  )
}

afterEach(() => vi.unstubAllGlobals())

describe("UserInventory", () => {
  it("shows local and remote NSS status", async () => {
    Element.prototype.getAnimations = () => [] as Animation[]
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  username: "local",
                  uid: 1000,
                  gid: 1000,
                  name: "Local",
                  home: "/home/local",
                  shell: "/bin/bash",
                  system: false,
                  groups: ["users"],
                  source: "local",
                  local: true,
                  mutable: true,
                },
                {
                  username: "remote",
                  uid: 2000,
                  gid: 2000,
                  name: "Remote",
                  home: "/home/remote",
                  shell: "/bin/false",
                  system: false,
                  groups: [],
                  source: "nss-read-only",
                  local: false,
                  mutable: false,
                },
              ],
            }),
            { status: 200, headers: { "Content-Type": "application/json" } }
          )
        )
      )
    )
    renderInventory()
    expect(
      await screen.findByText("2 identities · 1 local · 1 NSS read-only")
    ).toBeTruthy()
  })

  it("shows a bounded error state", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(new Response("", { status: 503 })))
    )
    renderInventory()
    expect(
      await screen.findByText("Identity inventory unavailable")
    ).toBeTruthy()
  })
})
