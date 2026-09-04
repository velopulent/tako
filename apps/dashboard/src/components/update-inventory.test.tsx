import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { UpdateInventory } from "@/components/update-inventory"

class EventSourceStub {
  addEventListener() {}
  close() {}
}

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

function status(fingerprint: string, externalLock = false) {
  return {
    available: true,
    backend: "apt",
    contract: "native-distro-provider",
    packages: [
      {
        name: "openssl",
        architecture: "amd64",
        currentVersion: "3.0.11",
        candidateVersion: "3.0.14",
      },
    ],
    fingerprint,
    externalLock,
    lockReason: externalLock ? "APT lock is held" : "",
    message: "1 installed-software update available.",
    recovery: { restartServices: [], hints: [], source: "advisory" },
  }
}

afterEach(() => vi.unstubAllGlobals())

describe("UpdateInventory", () => {
  it("shows a non-cancelable external package-manager lock", async () => {
    vi.stubGlobal("EventSource", EventSourceStub)
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const url = String(input)
        if (url.endsWith("/updates/history")) {
          return Promise.resolve(jsonResponse({ items: [], available: false }))
        }
        if (url.endsWith("/updates/kpatch")) {
          return Promise.resolve(
            jsonResponse({
              status: { supported: false, loaded: [], installed: [] },
              settings: {
                supported: false,
                missing: [],
                unavailable: [],
                auto: false,
                serviceEnabled: false,
                patchInstalled: false,
                patchUnavailable: false,
              },
            })
          )
        }
        return Promise.resolve(jsonResponse(status("b".repeat(64), true)))
      })
    )

    renderInventory()
    expect(await screen.findByText("Package manager busy")).toBeTruthy()
    expect(screen.getByText("APT lock is held")).toBeTruthy()
    expect(
      screen
        .getByRole("button", { name: "Preview full update" })
        .hasAttribute("disabled")
    ).toBe(true)
  })

  it("submits only the confirmed full-system plan fingerprint", async () => {
    const inventoryFingerprint = "a".repeat(64)
    const planFingerprint = "c".repeat(64)
    vi.stubGlobal("EventSource", EventSourceStub)
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/auth/session")) {
        return Promise.resolve(jsonResponse({ csrfToken: "csrf-token" }))
      }
      if (url.endsWith("/updates/history")) {
        return Promise.resolve(jsonResponse({ items: [], available: false }))
      }
      if (url.endsWith("/updates/kpatch")) {
        return Promise.resolve(
          jsonResponse({
            status: { supported: false, loaded: [], installed: [] },
            settings: {
              supported: false,
              missing: [],
              unavailable: [],
              auto: false,
              serviceEnabled: false,
              patchInstalled: false,
              patchUnavailable: false,
            },
          })
        )
      }
      if (url.endsWith("/updates/preview")) {
        return Promise.resolve(
          jsonResponse({
            current: status(inventoryFingerprint),
            changes: [
              {
                action: "upgrade",
                name: "openssl",
                currentVersion: "3.0.11",
                candidateVersion: "3.0.14",
              },
            ],
            warnings: [],
            fingerprint: planFingerprint,
            stale: false,
            allowed: true,
            requiresConfirmation: true,
            requiresRiskConfirmation: false,
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
      return Promise.resolve(jsonResponse(status(inventoryFingerprint)))
    })
    vi.stubGlobal("fetch", fetchMock)

    const user = userEvent.setup()
    renderInventory()
    await user.click(
      await screen.findByRole("button", { name: "Preview full update" })
    )
    expect(await screen.findByText("Confirm full-system update")).toBeTruthy()
    await user.click(screen.getByRole("button", { name: "Apply full update" }))

    await vi.waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).endsWith("/updates") && init?.method === "POST"
      )
      expect(call).toBeTruthy()
      const body = JSON.parse(String(call?.[1]?.body))
      expect(body).toEqual({
        expectedFingerprint: planFingerprint,
        confirmed: true,
        riskAccepted: false,
      })
      expect(body).not.toHaveProperty("scope")
      expect(body).not.toHaveProperty("packages")
    })
  })
})
