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
  localStorage.clear()
  document.documentElement.classList.remove("dark")
})

describe("SettingsPage", () => {
  it("reads the monitoring default from this browser", async () => {
    localStorage.setItem("tako-monitoring-default:v1", "30s")
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input)
        if (path.endsWith("/capabilities"))
          return Promise.resolve(jsonResponse({ capabilities: [] }))
        return Promise.resolve(jsonResponse({ csrfToken: "token" }))
      })
    )

    renderSettings()

    expect((await screen.findByRole("combobox")).textContent).toContain("30 seconds")
  })

  it("saves a keyboard-selected interval locally", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith("/capabilities"))
        return jsonResponse({
          capabilities: [
            {
              id: "services",
              state: "ready",
              backend: "systemd",
              readable: true,
              mutable: true,
              rollback: false,
              readAuthority: "session",
              mutationAuthority: "administrative",
              contract: "dbus",
            },
          ],
        })
      return jsonResponse({ csrfToken: "csrf-token" })
    })
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
      expect(localStorage.getItem("tako-monitoring-default:v1")).toBe("30s")
    })
    expect(
      fetchMock.mock.calls.some(
        (call) =>
          (call as [RequestInfo | URL, RequestInit?])[1]?.method === "PUT"
      )
    ).toBe(false)
    expect(screen.getByText("systemd")).toBeTruthy()
    expect(screen.getByText("Read · Write · No rollback")).toBeTruthy()
    expect(screen.getByText("Contract: dbus")).toBeTruthy()
  })
})
