import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { JobsPage } from "@/routes/jobs"

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function renderJobs() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <JobsPage />
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.documentElement.classList.remove("dark")
})

describe("JobsPage", () => {
  it("shows loading state while jobs reconnect", () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        if (String(input).endsWith("/jobs?limit=50")) {
          return new Promise<Response>(() => {})
        }
        return Promise.resolve(jsonResponse({ csrfToken: "csrf-token" }))
      })
    )
    const { container } = renderJobs()
    expect(container.querySelector('[data-slot="skeleton"]')).not.toBeNull()
  })

  it("supports keyboard start and shows durable empty-state workflow", async () => {
    document.documentElement.classList.add("dark")
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: 375,
    })
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith("/jobs?limit=50"))
        return Promise.resolve(jsonResponse({ items: [] }))
      if (path.endsWith("/jobs/host-inventory"))
        return Promise.resolve(
          jsonResponse(
            {
              job: {
                id: "inventory-1",
                kind: "host-inventory",
                actor: "operator",
                state: "pending",
                progress: 0,
                message: "Queued",
                createdAt: "2026-08-13T00:00:00Z",
                cancelRequested: false,
                dangerous: false,
              },
            },
            202
          )
        )
      if (init?.method === "POST")
        return Promise.resolve(jsonResponse({ job: {} }))
      return Promise.resolve(jsonResponse({ csrfToken: "csrf-token" }))
    })
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    renderJobs()

    expect(await screen.findByText("No diagnostic jobs yet")).toBeTruthy()
    const start = screen.getByRole("button", { name: "Run host inventory" })
    start.focus()
    await user.keyboard("{Enter}")
    await vi.waitFor(() => {
      const call = fetchMock.mock.calls.find(([input]) =>
        String(input).endsWith("/jobs/host-inventory")
      )
      expect(call).toBeTruthy()
      expect(call?.[1]?.headers).toMatchObject({ "X-CSRF-Token": "csrf-token" })
      expect(call?.[1]?.body).toBe("{}")
    })
    expect(document.documentElement.classList.contains("dark")).toBe(true)
  })
})
