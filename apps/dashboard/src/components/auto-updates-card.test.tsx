import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { AutoUpdatesCard } from "@/components/auto-updates-card"

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

const dnf5Config = {
  available: true,
  supported: true,
  installed: true,
  enabled: false,
  type: "all",
  day: "",
  time: "",
  provider: "dnf5-automatic",
  packageName: "dnf5-plugin-automatic",
}

function renderCard() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <AutoUpdatesCard csrfToken="csrf-token" administrative />
    </QueryClientProvider>
  )
}

afterEach(() => vi.unstubAllGlobals())

describe("AutoUpdatesCard", () => {
  it("shows state and applies a security schedule through PUT", async () => {
    const fetchMock = vi.fn((...call: [RequestInfo | URL, RequestInit?]) => {
      const url = String(call[0])
      if (url.endsWith("/updates/automatic")) {
        return Promise.resolve(jsonResponse(dnf5Config))
      }
      return Promise.resolve(jsonResponse({}))
    })
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    renderCard()

    expect(await screen.findByText("Automatic updates")).toBeTruthy()
    expect(screen.getByText("Disabled")).toBeTruthy()

    await user.click(screen.getByRole("button", { name: "Edit" }))
    await user.click(
      await screen.findByRole("button", { name: "Security only" })
    )
    await user.click(screen.getByRole("button", { name: "Save changes" }))

    await vi.waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).endsWith("/updates/automatic") && init?.method === "PUT"
      )
      expect(call).toBeTruthy()
      const body = JSON.parse(String(call?.[1]?.body))
      expect(body.enabled).toBe(true)
      expect(body.type).toBe("security")
    })
  })

  it("reports unavailable providers honestly", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve(
          jsonResponse({
            available: false,
            supported: false,
            installed: false,
            enabled: false,
            type: "all",
            day: "",
            time: "",
          })
        )
      )
    )
    renderCard()
    expect(await screen.findByText("Not available")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull()
  })
})
