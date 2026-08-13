import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { HostPage } from "@/routes/host"

const current = {
  hostname: "tako",
  timezone: "UTC",
  ntpEnabled: true,
  fingerprint: "a".repeat(64),
}

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function renderHost() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <HostPage />
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.documentElement.classList.remove("dark")
})

describe("HostPage", () => {
  it("shows loading and read-only states", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        if (String(input).endsWith("/host/config"))
          return new Promise<Response>(() => {})
        return Promise.resolve(jsonResponse({ csrfToken: "csrf" }))
      })
    )
    const { container } = renderHost()
    expect(container.querySelector('[data-slot="skeleton"]')).not.toBeNull()
  })

  it("previews keyboard-edited changes and keeps writes gated", async () => {
    document.documentElement.classList.add("dark")
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: 375,
    })
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith("/host/config/preview"))
        return Promise.resolve(
          jsonResponse({
            current,
            proposed: { ...current, hostname: "new-host" },
            changes: ["hostname"],
            stale: false,
          })
        )
      if (path.endsWith("/host/config")) {
        if (init?.method === "PUT")
          return Promise.resolve(jsonResponse(current))
        return Promise.resolve(jsonResponse(current))
      }
      if (path.endsWith("/admin"))
        return Promise.resolve(jsonResponse({ administrative: false }))
      return Promise.resolve(jsonResponse({ csrfToken: "csrf-token" }))
    })
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    renderHost()

    const hostname = await screen.findByLabelText("Hostname")
    await user.clear(hostname)
    await user.type(hostname, "new-host")
    await user.click(screen.getByRole("button", { name: "Preview changes" }))
    expect(await screen.findByText("Changes: hostname.")).toBeTruthy()
    const previewCall = fetchMock.mock.calls.find(([input]) =>
      String(input).endsWith("/host/config/preview")
    )
    expect(previewCall?.[1]?.body).toBe(
      JSON.stringify({
        hostname: "new-host",
        timezone: "UTC",
        ntpEnabled: true,
        expectedFingerprint: current.fingerprint,
      })
    )
    expect(
      (
        screen.getByRole("button", {
          name: "Apply and verify",
        }) as HTMLButtonElement
      ).disabled
    ).toBe(true)
    expect(document.documentElement.classList.contains("dark")).toBe(true)
  })
})
