import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TimersPage } from "@/routes/timers"

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function renderTimers() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <TimersPage />
    </QueryClientProvider>
  )
}

afterEach(() => vi.unstubAllGlobals())

describe("TimersPage", () => {
  it("keeps the form loading while the session is pending", () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<Response>(() => {}))
    )
    const { container } = renderTimers()
    expect(container.querySelector('[data-slot="skeleton"]')).not.toBeNull()
    expect(screen.queryByText("Timer definition")).toBeNull()
  })

  it("shows session errors", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(jsonResponse({ code: "unavailable" }, 503)))
    )
    renderTimers()
    expect(await screen.findByText("Timer controls unavailable")).toBeTruthy()
  })

  it("previews a timer with keyboard-accessible fields", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        void init
        const path = String(input)
        if (path.endsWith("/auth/session")) {
          return jsonResponse({
            user: {
              username: "octopus",
              name: "Octopus",
              uid: 1000,
              gid: 1000,
            },
            csrfToken: "csrf-token",
            administrative: false,
          })
        }
        return jsonResponse({
          scope: "user",
          name: "nightly",
          timerUnit: "nightly.timer",
          serviceUnit: "nightly.service",
          exists: false,
          enabled: false,
        })
      }
    )
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    renderTimers()

    expect(
      ((await screen.findByLabelText("Timer name")) as HTMLInputElement).value
    ).toBe("nightly")
    expect(
      (
        screen.getByRole("textbox", {
          name: "Command",
        }) as HTMLTextAreaElement
      ).value
    ).toBe("/usr/local/bin/backup")
    await user.click(
      screen.getByRole("button", { name: "Preview current state" })
    )
    expect(await screen.findByText("No timer files yet")).toBeTruthy()
    const previewCall = fetchMock.mock.calls.find(([input]) =>
      String(input).endsWith("/timers/preview")
    )
    expect(previewCall).toBeTruthy()
    expect(JSON.parse(String(previewCall?.[1]?.body))).toMatchObject({
      action: "create",
      scope: "user",
      name: "nightly",
    })
  })
})
