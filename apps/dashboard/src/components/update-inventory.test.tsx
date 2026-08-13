import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
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

  it("previews selected packages and starts a reconnectable job", async () => {
    const fingerprint = "a".repeat(64)
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/auth/session")) {
        return Promise.resolve(jsonResponse({ csrfToken: "csrf-token" }))
      }
      if (url.endsWith("/updates/preview")) {
        return Promise.resolve(
          jsonResponse({
            operation: { scope: "selected", packages: ["openssl"] },
            current: {
              available: true,
              backend: "apt-get",
              contract: "bounded-command-read-only",
              packages: [],
              fingerprint,
              externalLock: false,
              message: "",
            },
            selected: [{ name: "openssl", candidateVersion: "3.0.14" }],
            changes: ["update 1 package"],
            warnings: [],
            fingerprint,
            stale: false,
            allowed: true,
            requiresConfirmation: true,
          })
        )
      }
      if (url.endsWith("/updates") && init?.method === "POST") {
        return Promise.resolve(
          jsonResponse(
            {
              job: {
                id: "job-1",
                state: "pending",
                progress: 0,
                message: "Queued",
              },
            },
            202
          )
        )
      }
      return Promise.resolve(
        jsonResponse({
          available: true,
          backend: "apt-get",
          version: "apt 3.0",
          contract: "bounded-command-read-only",
          fingerprint,
          packages: [{ name: "openssl", candidateVersion: "3.0.14" }],
          externalLock: false,
          message: "1 installed-software update available.",
        })
      )
    })
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    renderInventory()
    await user.click(
      await screen.findByRole("button", { name: "Selected packages" })
    )
    await user.click(screen.getByRole("checkbox", { name: "Select openssl" }))
    await user.click(screen.getByRole("button", { name: "Preview updates" }))
    expect(await screen.findByText("Update preview")).toBeTruthy()
    await user.type(
      screen.getByLabelText("Update confirmation"),
      "APPLY UPDATES"
    )
    await user.click(screen.getByRole("button", { name: "Apply updates" }))
    await vi.waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).endsWith("/updates") && init?.method === "POST"
      )
      expect(call).toBeTruthy()
      expect(JSON.parse(String(call?.[1]?.body))).toMatchObject({
        scope: "selected",
        packages: ["openssl"],
        confirmation: "APPLY UPDATES",
        expectedFingerprint: fingerprint,
      })
    })
  })
})
