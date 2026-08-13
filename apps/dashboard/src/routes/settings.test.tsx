import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { SettingsPage } from "@/routes/settings"

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function renderSettings() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <SettingsPage />
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.documentElement.classList.remove("dark")
})

describe("SettingsPage", () => {
  it("keeps the monitoring field pending while the server preference loads", async () => {
    const never = new Promise<Response>(() => {})
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input)
        if (path.endsWith("/preferences/monitoring")) return never
        if (path.endsWith("/capabilities"))
          return Promise.resolve(jsonResponse({ capabilities: [] }))
        return Promise.resolve(jsonResponse({ csrfToken: "token" }))
      })
    )

    const { container } = renderSettings()

    expect(container.querySelector('[data-slot="skeleton"]')).not.toBeNull()
    expect(screen.queryByText("Default refresh interval")).toBeNull()
  })

  it("shows preference errors and the empty capability state", async () => {
    document.documentElement.classList.add("dark")
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: 375,
    })
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input)
        if (path.endsWith("/preferences/monitoring"))
          return Promise.resolve(jsonResponse({ code: "unavailable" }, 500))
        if (path.endsWith("/capabilities"))
          return Promise.resolve(jsonResponse({ capabilities: [] }))
        return Promise.resolve(jsonResponse({ csrfToken: "token" }))
      })
    )

    renderSettings()

    expect(
      await screen.findByText("Could not load monitoring preference")
    ).toBeTruthy()
    expect(
      await screen.findByText("No optional capabilities detected")
    ).toBeTruthy()
  })

  it("saves a keyboard-selected interval with the current revision", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input)
        if (path.endsWith("/preferences/monitoring") && init?.method === "PUT")
          return jsonResponse({ defaultInterval: "30s", revision: 4 })
        if (path.endsWith("/preferences/monitoring"))
          return jsonResponse({ defaultInterval: "1m", revision: 3 })
        if (path.endsWith("/capabilities"))
          return jsonResponse({
            capabilities: [{ id: "systemd", available: true }],
          })
        return jsonResponse({ csrfToken: "csrf-token" })
      }
    )
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    renderSettings()

    const select = await screen.findByRole("combobox", {
      name: "Refresh interval",
    })
    select.focus()
    await user.keyboard("{Enter}")
    await user.keyboard("30s")
    await user.keyboard("{Enter}")

    await vi.waitFor(() => {
      const put = fetchMock.mock.calls.find(
        ([, init]) => init?.method === "PUT"
      )
      expect(put).toBeTruthy()
      expect(put?.[1]?.body).toBe(
        JSON.stringify({ defaultInterval: "30s", expectedRevision: 3 })
      )
    })
  })
})
