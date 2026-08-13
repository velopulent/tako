import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { SavedLogViews } from "@/components/saved-log-views"

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function renderViews(onApply = vi.fn()) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return {
    onApply,
    ...render(
      <QueryClientProvider client={client}>
        <SavedLogViews filter={{ unit: "worker.service" }} onApply={onApply} />
      </QueryClientProvider>
    ),
  }
}

afterEach(() => vi.unstubAllGlobals())

describe("SavedLogViews", () => {
  it("applies, creates, edits, and deletes an owned view", async () => {
    Element.prototype.getAnimations = () => [] as Animation[]
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input)
        if (url.endsWith("/auth/session")) {
          return jsonResponse({ csrfToken: "csrf" })
        }
        if (url.endsWith("/log-views") && init?.method === "POST") {
          return jsonResponse({ id: "view-2", name: "New", revision: 1 })
        }
        if (url.includes("/log-views/view-1") && init?.method === "PUT") {
          return jsonResponse({ id: "view-1", name: "Incident", revision: 2 })
        }
        if (url.includes("/log-views/view-1") && init?.method === "DELETE") {
          return new Response(null, { status: 204 })
        }
        return jsonResponse({
          items: [
            {
              id: "view-1",
              name: "Incident",
              filter: { unit: "worker.service", priority: "3" },
              revision: 1,
              createdAt: "2026-08-13T00:00:00Z",
              updatedAt: "2026-08-13T00:00:00Z",
            },
          ],
        })
      }
    )
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    const { onApply } = renderViews()
    await user.click(await screen.findByRole("button", { name: "Apply" }))
    expect(onApply).toHaveBeenCalledWith({
      unit: "worker.service",
      priority: "3",
    })

    await user.type(screen.getByLabelText("Save current filters"), "New")
    await user.click(screen.getByRole("button", { name: "Save view" }))
    await vi.waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([, init]) => init?.method === "POST")
      ).toBe(true)
    )

    await user.click(screen.getByRole("button", { name: "Rename" }))
    const rename = screen.getByRole("textbox", { name: "Rename Incident" })
    await user.clear(rename)
    await user.type(rename, "Incident renamed")
    await user.click(screen.getByRole("button", { name: "Save name" }))
    await vi.waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([, init]) => init?.method === "PUT")
      ).toBe(true)
    )
    await user.click(screen.getByRole("button", { name: "Delete" }))
    await vi.waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([, init]) => init?.method === "DELETE")
      ).toBe(true)
    )
  })
})
