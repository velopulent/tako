import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router"

import { JournalBrowser } from "@/components/journal-browser"
import { logsSearch } from "@/lib/search"

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function createTestRouter() {
  const rootRoute = createRootRoute()
  const logsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/logs",
    validateSearch: logsSearch,
    component: JournalBrowser,
  })
  const routeTree = rootRoute.addChildren([logsRoute])
  const history = createMemoryHistory({ initialEntries: ["/logs"] })
  return createRouter({ routeTree, history })
}

function renderJournal() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const router = createTestRouter()
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

afterEach(() => vi.unstubAllGlobals())

describe("JournalBrowser", () => {
  it("shows loading and error states", async () => {
    const never = new Promise<Response>(() => {})
    vi.stubGlobal(
      "fetch",
      vi.fn(() => never)
    )
    const { container } = renderJournal()
    await vi.waitFor(() =>
      expect(container.querySelector('[data-slot="skeleton"]')).not.toBeNull()
    )

    vi.unstubAllGlobals()
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(jsonResponse({ code: "unavailable" }, 503)))
    )
    renderJournal()
    expect(await screen.findByText("Journal unavailable")).toBeTruthy()
  })

  it("filters through the API and paginates with an opaque cursor", async () => {
    Element.prototype.getAnimations = () => [] as Animation[]
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("cursor=")) {
        return jsonResponse({
          items: [
            {
              timestamp: "2023-11-14T22:13:20Z",
              priority: "3",
              unit: "worker.service",
              message: "older",
            },
          ],
        })
      }
      return jsonResponse({
        items: [
          {
            timestamp: "2023-11-14T22:13:21Z",
            priority: "3",
            unit: "worker.service",
            message: "failed",
            details: { _PID: "42" },
          },
        ],
        nextCursor: "opaque-cursor",
      })
    })
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    renderJournal()
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled())
    await user.type(screen.getByLabelText("Message contains"), "failed")
    await vi.waitFor(() => {
      const request = fetchMock.mock.calls.find(([input]) =>
        String(input).includes("text=failed")
      )
      expect(request).toBeTruthy()
    })
    expect(
      (await screen.findByRole("link", { name: "Export CSV" })).getAttribute(
        "href"
      )
    ).toContain("text=failed")
    await user.click(screen.getByRole("button", { name: "Details off" }))
    await vi.waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input]) =>
          String(input).includes("details=true")
        )
      ).toBe(true)
    })
    await user.click(
      await screen.findByRole("button", { name: "Older entries" })
    )
    await vi.waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input]) =>
          String(input).includes("cursor=")
        )
      ).toBe(true)
    })
  })

  it("shows a bounded empty state", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(jsonResponse({ items: [] })))
    )
    renderJournal()
    expect(await screen.findByText("No journal entries")).toBeTruthy()
  })
})
