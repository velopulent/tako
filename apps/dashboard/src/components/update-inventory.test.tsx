import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { UpdateInventory } from "@/components/update-inventory"

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function renderInventory() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <UpdateInventory />
    </QueryClientProvider>
  )
}

afterEach(() => vi.unstubAllGlobals())

describe("UpdateInventory", () => {
  it("shows bounded package versions and external lock state", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve(
          jsonResponse({
            available: true,
            backend: "apt-get",
            version: "apt 3.0",
            contract: "bounded-command-read-only",
            packages: [
              {
                name: "openssl",
                architecture: "amd64",
                currentVersion: "3.0.11",
                candidateVersion: "3.0.14",
                severity: "security",
                size: 1024,
                summary: "TLS update",
              },
            ],
            externalLock: true,
            lockReason: "A package-manager lock is held",
            message: "1 installed-software update available.",
          })
        )
      )
    )
    renderInventory()
    expect(await screen.findByText("openssl (amd64)")).toBeTruthy()
    expect(screen.getByText("3.0.14")).toBeTruthy()
    expect(screen.getByText("security")).toBeTruthy()
    expect(screen.getByText("Another package tool holds a lock")).toBeTruthy()
  })

  it("renders an explicit empty state and backend failure", async () => {
    let failed = true
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve(
          failed
            ? new Response("", { status: 503 })
            : jsonResponse({
                available: true,
                backend: "PackageKit",
                contract: "dbus-read-only",
                packages: [],
                externalLock: false,
                message:
                  "No installed-software updates are currently available.",
              })
        )
      )
    )
    renderInventory()
    expect(await screen.findByText("Update inventory unavailable")).toBeTruthy()
    failed = false
    renderInventory()
    expect(
      await screen.findByText("No installed-software updates")
    ).toBeTruthy()
  })
})
