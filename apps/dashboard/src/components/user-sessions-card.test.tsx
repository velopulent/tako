import { screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { jsonResponse, renderWithProviders } from "@/test/render"
import { describeAgent, UserSessionsCard } from "./user-sessions-card"

const sessions = [
  {
    id: "current-id",
    createdAt: new Date().toISOString(),
    seenAt: new Date().toISOString(),
    current: true,
    administrative: false,
    remoteAddress: "192.0.2.4",
    userAgent:
      "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0",
  },
  {
    id: "phone-id",
    createdAt: new Date().toISOString(),
    seenAt: new Date().toISOString(),
    current: false,
    administrative: true,
    remoteAddress: "198.51.100.7",
    userAgent:
      "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1",
  },
]

afterEach(() => vi.unstubAllGlobals())

describe("UserSessionsCard", () => {
  it("lists sessions and signs out another one", async () => {
    const fetch = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        if (String(input).endsWith("/revoke") && init?.method === "POST")
          return new Response(null, { status: 204 })
        return jsonResponse({ sessions })
      }
    )
    vi.stubGlobal("fetch", fetch)
    renderWithProviders(<UserSessionsCard />)

    expect(await screen.findByText("Firefox on Linux")).toBeTruthy()
    expect(screen.getByText("Safari on iOS")).toBeTruthy()
    expect(screen.getByText("This browser")).toBeTruthy()
    expect(screen.getByText("Administrative")).toBeTruthy()

    await userEvent.click(
      screen.getByRole("button", { name: "Sign out session" })
    )
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([input, init]) =>
            String(input) === "/api/v1/auth/sessions/phone-id/revoke" &&
            init?.method === "POST"
        )
      ).toBe(true)
    )
  })

  it("shows a retryable error when the list fails", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse({ code: "service-unavailable" }, 503))
    )
    renderWithProviders(<UserSessionsCard />)
    expect(await screen.findByText("Could not load this data")).toBeTruthy()
  })

  it("describes unknown agents without guessing", () => {
    expect(describeAgent(undefined)).toBe("Unknown browser")
    expect(describeAgent("curl/8.9.1")).toBe("curl/8.9.1")
  })
})
