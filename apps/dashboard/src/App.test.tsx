import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import App from "./App"

vi.mock("@tanstack/react-router", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-router")>(
    "@tanstack/react-router"
  )
  const React = await import("react")

  return {
    ...actual,
    RouterProvider: () =>
      React.createElement("div", { "data-testid": "dashboard-shell" }),
  }
})

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

async function renderApp() {
  return render(<App />)
}

afterEach(() => vi.unstubAllGlobals())

describe("App session gate", () => {
  it("waits for auth before showing login, then enters the dashboard", async () => {
    let resolveSession!: (response: Response) => void
    const pendingSession = new Promise<Response>((resolve) => {
      resolveSession = resolve
    })
    const authenticatedSession = {
      user: {
        username: "octopus",
        name: "Octopus",
        uid: 1000,
        gid: 1000,
      },
      csrfToken: "csrf-token",
      administrative: false,
    }

    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input)
        void init
        if (path.endsWith("/auth/session")) {
          return pendingSession
        }
        if (path.endsWith("/branding")) {
          return Promise.resolve(
            jsonResponse({
              distribution: "linux",
              hostname: "server-01",
              backgroundUrl: "/branding/server-01.png",
            })
          )
        }
        if (path.endsWith("/auth/login")) {
          return Promise.resolve(jsonResponse(authenticatedSession))
        }
        return Promise.reject(new Error(`Unexpected request: ${path}`))
      })
    )

    await renderApp()

    expect(screen.getByTestId("auth-pending-shell")).toBeTruthy()
    expect(screen.queryByRole("status")).toBeNull()
    expect(screen.queryByText(/Checking session/)).toBeNull()
    expect(screen.queryByText(/Login to/)).toBeNull()
    expect(screen.queryByLabelText("Username")).toBeNull()

    resolveSession(jsonResponse({ code: "unauthorized" }, 401))
    expect(await screen.findByLabelText("Username")).toBeTruthy()
    expect(screen.getByText("Login to server-01")).toBeTruthy()
    expect(
      document.querySelector('img[src="/branding/server-01.png"]')
    ).not.toBeNull()

    const user = userEvent.setup()
    await user.type(screen.getByLabelText("Username"), "octopus")
    await user.type(screen.getByLabelText("Password"), "secret")
    await user.click(screen.getByRole("button", { name: "Sign in" }))

    expect(await screen.findByTestId("dashboard-shell")).toBeTruthy()
    expect(screen.queryByLabelText("Username")).toBeNull()
    expect(screen.queryByText(/Login to/)).toBeNull()
  })
})
