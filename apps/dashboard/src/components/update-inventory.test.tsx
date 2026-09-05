import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { UpdateInventory } from "@/components/update-inventory"

class EventSourceStub {
  static instances: EventSourceStub[] = []
  readonly url: string
  private listeners = new Map<
    string,
    Array<(event: MessageEvent<string>) => void>
  >()

  constructor(url: string) {
    this.url = url
    EventSourceStub.instances.push(this)
  }

  addEventListener(
    type: string,
    listener: (event: MessageEvent<string>) => void
  ) {
    const listeners = this.listeners.get(type) ?? []
    listeners.push(listener)
    this.listeners.set(type, listeners)
  }

  emit(type: string, value: unknown) {
    const event = { data: JSON.stringify(value) } as MessageEvent<string>
    for (const listener of this.listeners.get(type) ?? []) listener(event)
  }

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

function status(
  fingerprint: string,
  externalLock = false,
  packages = [
    {
      name: "openssl",
      architecture: "amd64",
      currentVersion: "3.0.11",
      candidateVersion: "3.0.14",
    },
  ]
) {
  return {
    available: true,
    backend: "apt",
    contract: "native-distro-provider",
    packages,
    fingerprint,
    externalLock,
    lockReason: externalLock ? "APT lock is held" : "",
    message: "1 installed-software update available.",
    recovery: { restartServices: [], hints: [], source: "advisory" },
  }
}

afterEach(() => {
  sessionStorage.clear()
  EventSourceStub.instances = []
  vi.unstubAllGlobals()
})

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

  it("shows stale preview warnings and requires explicit risk confirmation", async () => {
    vi.stubGlobal("EventSource", EventSourceStub)
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const url = String(input)
        if (url.endsWith("/auth/session")) {
          return Promise.resolve(
            jsonResponse({ csrfToken: "csrf-token", administrative: true })
          )
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
              current: status("5".repeat(64)),
              changes: [
                {
                  action: "remove",
                  name: "legacy-package",
                  currentVersion: "1.0",
                },
              ],
              warnings: ["The inventory changed while previewing."],
              fingerprint: "6".repeat(64),
              stale: true,
              allowed: false,
              requiresConfirmation: true,
              requiresRiskConfirmation: true,
              reason: "Refresh is required.",
            })
          )
        }
        return Promise.resolve(jsonResponse(status("5".repeat(64))))
      })
    )

    const user = userEvent.setup()
    renderInventory()
    await user.click(
      await screen.findByRole("button", { name: "Preview full update" })
    )
    expect(await screen.findByText("Inventory changed")).toBeTruthy()
    expect(screen.getByRole("checkbox")).toBeTruthy()
    expect(
      screen
        .getByRole("button", { name: "Apply full update" })
        .hasAttribute("disabled")
    ).toBe(true)
  })

  it("renders one live output control and suppresses duplicate SSE sequences", async () => {
    vi.stubGlobal("EventSource", EventSourceStub)
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const url = String(input)
        if (url.endsWith("/auth/session")) {
          return Promise.resolve(
            jsonResponse({ csrfToken: "csrf-token", administrative: true })
          )
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
        return Promise.resolve(jsonResponse(status("d".repeat(64))))
      })
    )

    const user = userEvent.setup()
    renderInventory()
    const source = await vi.waitFor(() => {
      const instance = EventSourceStub.instances[0]
      if (!instance) throw new Error("EventSource was not created")
      return instance
    })

    source.emit("progress", {
      sequence: 10,
      jobId: "refresh-1",
      active: true,
      phase: "refreshing",
      current: 1,
      total: 2,
      percent: 50,
      message: "Refreshing package metadata",
      cancelable: false,
      timestamp: new Date().toISOString(),
    })
    source.emit("progress", {
      sequence: 9,
      jobId: "refresh-1",
      active: true,
      phase: "refreshing",
      current: 0,
      total: 2,
      percent: 0,
      message: "stale replay",
      cancelable: false,
      timestamp: new Date().toISOString(),
    })
    source.emit("output", {
      sequence: 11,
      jobId: "refresh-1",
      stream: "stdout",
      line: "Reading package lists...",
      timestamp: new Date().toISOString(),
    })
    source.emit("output", {
      sequence: 11,
      jobId: "refresh-1",
      stream: "stdout",
      line: "duplicate replay",
      timestamp: new Date().toISOString(),
    })

    expect(await screen.findByText("Update activity")).toBeTruthy()
    expect(screen.queryByText("stale replay")).toBeNull()
    expect(
      screen.getAllByRole("button", { name: /View live output/ })
    ).toHaveLength(1)
    await user.click(screen.getByRole("button", { name: /View live output/ }))
    expect(
      await screen.findByText("stdout: Reading package lists...")
    ).toBeTruthy()
    expect(screen.queryByText("duplicate replay")).toBeNull()
  })

  it("clears a successful job, removes storage, and refreshes inventory and history", async () => {
    const inventoryFingerprint = "e".repeat(64)
    const planFingerprint = "f".repeat(64)
    let inventoryCalls = 0
    let historyCalls = 0
    vi.stubGlobal("EventSource", EventSourceStub)
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/auth/session")) {
        return Promise.resolve(
          jsonResponse({ csrfToken: "csrf-token", administrative: true })
        )
      }
      if (url.endsWith("/updates/history")) {
        historyCalls += 1
        return Promise.resolve(
          jsonResponse({
            items:
              historyCalls > 1
                ? [{ time: Date.now(), packages: { openssl: "3.0.14" } }]
                : [],
            available: true,
          })
        )
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
                kind: "software-update",
                actor: "tester",
                state: "pending",
                progress: 0,
                message: "Queued",
                cancelRequested: false,
                dangerous: true,
                createdAt: new Date().toISOString(),
              },
            },
            202
          )
        )
      }
      if (url.endsWith("/jobs/job-1")) {
        return Promise.resolve(
          jsonResponse({
            job: {
              id: "job-1",
              kind: "software-update",
              actor: "tester",
              state: "pending",
              progress: 0,
              message: "Queued",
              cancelRequested: false,
              dangerous: true,
              createdAt: new Date().toISOString(),
            },
          })
        )
      }
      if (url.endsWith("/updates")) {
        inventoryCalls += 1
        return Promise.resolve(
          jsonResponse(
            inventoryCalls > 1
              ? {
                  ...status(inventoryFingerprint, false, []),
                  message: "No installed-software updates are waiting.",
                }
              : status(inventoryFingerprint)
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
    await user.click(
      await screen.findByRole("button", { name: "Apply full update" })
    )
    await vi.waitFor(() =>
      expect(sessionStorage.getItem("tako-update-job:v2")).toBe("job-1")
    )

    const source = EventSourceStub.instances[0]
    if (!source) throw new Error("EventSource was not created")
    source.emit("progress", {
      sequence: 20,
      jobId: "job-1",
      active: false,
      phase: "completed",
      current: 1,
      total: 1,
      percent: 100,
      message: "Update complete",
      cancelable: false,
      timestamp: new Date().toISOString(),
    })
    source.emit("progress", {
      sequence: 19,
      jobId: "job-1",
      active: true,
      phase: "applying",
      current: 0,
      total: 1,
      percent: 0,
      message: "stale terminal replay",
      cancelable: false,
      timestamp: new Date().toISOString(),
    })

    await vi.waitFor(() => {
      expect(sessionStorage.getItem("tako-update-job:v2")).toBeNull()
      expect(inventoryCalls).toBeGreaterThan(1)
      expect(historyCalls).toBeGreaterThan(1)
    })
    expect(await screen.findByText("System is up to date")).toBeTruthy()
    expect(screen.queryByText("Update activity")).toBeNull()
    expect(screen.queryByText("stale terminal replay")).toBeNull()
  })

  it("shows one retryable failure alert and clears failed live state", async () => {
    vi.stubGlobal("EventSource", EventSourceStub)
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const url = String(input)
        if (url.endsWith("/auth/session")) {
          return Promise.resolve(
            jsonResponse({ csrfToken: "csrf-token", administrative: true })
          )
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
        return Promise.resolve(jsonResponse(status("1".repeat(64))))
      })
    )

    renderInventory()
    const source = await vi.waitFor(() => {
      const instance = EventSourceStub.instances[0]
      if (!instance) throw new Error("EventSource was not created")
      return instance
    })
    source.emit("progress", {
      sequence: 30,
      jobId: "job-failed",
      active: true,
      phase: "running",
      current: 1,
      total: 2,
      percent: 50,
      message: "Applying updates",
      cancelable: false,
      timestamp: new Date().toISOString(),
    })
    expect(await screen.findByText("Update activity")).toBeTruthy()
    source.emit("progress", {
      sequence: 31,
      jobId: "job-failed",
      active: false,
      phase: "failed",
      current: 1,
      total: 2,
      percent: 50,
      message: "Package transaction failed",
      cancelable: false,
      timestamp: new Date().toISOString(),
    })

    expect(await screen.findByText("Package transaction failed")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Check again" })).toBeTruthy()
    expect(screen.queryByText("Update activity")).toBeNull()
    expect(sessionStorage.getItem("tako-update-job:v2")).toBeNull()
  })

  it("cancels only a tracked update before the package commit starts", async () => {
    sessionStorage.setItem("tako-update-job:v2", "job-cancel")
    vi.stubGlobal("EventSource", EventSourceStub)
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/auth/session")) {
        return Promise.resolve(
          jsonResponse({ csrfToken: "csrf-token", administrative: true })
        )
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
      if (url.endsWith("/jobs/job-cancel")) {
        return Promise.resolve(
          jsonResponse({
            job: {
              id: "job-cancel",
              kind: "software-update",
              actor: "tester",
              state: "running",
              progress: 10,
              message: "Downloading packages",
              cancelRequested: false,
              dangerous: true,
              createdAt: new Date().toISOString(),
            },
          })
        )
      }
      if (url.endsWith("/jobs/job-cancel/cancel") && init?.method === "POST") {
        return Promise.resolve(
          jsonResponse({
            job: {
              id: "job-cancel",
              kind: "software-update",
              actor: "tester",
              state: "canceled",
              progress: 10,
              message: "Canceled by operator",
              cancelRequested: true,
              dangerous: true,
              createdAt: new Date().toISOString(),
            },
          })
        )
      }
      return Promise.resolve(jsonResponse(status("4".repeat(64))))
    })
    vi.stubGlobal("fetch", fetchMock)

    const user = userEvent.setup()
    renderInventory()
    expect(await screen.findByText("Update activity")).toBeTruthy()
    await user.click(screen.getByRole("button", { name: "Cancel update" }))

    await vi.waitFor(() => {
      expect(
        fetchMock.mock.calls.some(
          ([input, init]) =>
            String(input).endsWith("/jobs/job-cancel/cancel") &&
            init?.method === "POST"
        )
      ).toBe(true)
      expect(sessionStorage.getItem("tako-update-job:v2")).toBeNull()
    })
    expect(screen.queryByText("Update activity")).toBeNull()
  })

  it("clears terminal and missing persisted jobs after reload", async () => {
    sessionStorage.setItem("tako-update-job:v2", "old-job")
    vi.stubGlobal("EventSource", EventSourceStub)
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const url = String(input)
        if (url.endsWith("/jobs/old-job")) {
          return Promise.resolve(jsonResponse({ message: "not found" }, 404))
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
        return Promise.resolve(
          jsonResponse({
            ...status("2".repeat(64), false, []),
            message: "No installed-software updates are waiting.",
          })
        )
      })
    )

    renderInventory()
    expect(
      await screen.findByRole("button", { name: "No updates available" })
    ).toBeTruthy()
    await vi.waitFor(() =>
      expect(sessionStorage.getItem("tako-update-job:v2")).toBeNull()
    )
    expect(screen.queryByText("Update activity")).toBeNull()
  })

  it("shows supported kernel live patching inline without a settings card", async () => {
    vi.stubGlobal("EventSource", EventSourceStub)
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const url = String(input)
        if (url.endsWith("/auth/session")) {
          return Promise.resolve(
            jsonResponse({ csrfToken: "csrf-token", administrative: true })
          )
        }
        if (url.endsWith("/updates/history")) {
          return Promise.resolve(jsonResponse({ items: [], available: false }))
        }
        if (url.endsWith("/updates/kpatch")) {
          return Promise.resolve(
            jsonResponse({
              status: { supported: true, loaded: [], installed: [] },
              settings: {
                supported: true,
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
        return Promise.resolve(jsonResponse(status("3".repeat(64))))
      })
    )

    renderInventory()
    expect(await screen.findByText("Kernel live patching")).toBeTruthy()
    expect(screen.queryByText("Settings")).toBeNull()
  })
})
