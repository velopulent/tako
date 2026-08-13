import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { AccountLoginHistory } from "@/components/account-login-history"

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function renderHistory() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <AccountLoginHistory />
    </QueryClientProvider>
  )
}

afterEach(() => vi.unstubAllGlobals())

describe("AccountLoginHistory", () => {
  it("renders identity source and bounded event fields", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input)
        if (path.endsWith("/auth/session")) {
          return Promise.resolve(
            jsonResponse({
              user: { username: "operator", uid: 1000, gid: 1000 },
              csrfToken: "csrf",
              administrative: false,
            })
          )
        }
        return Promise.resolve(
          jsonResponse({
            identity: {
              username: "operator",
              source: "local",
              present: true,
            },
            items: [
              {
                timestamp: "2024-01-01T00:00:00Z",
                event: "login",
                outcome: "success",
                service: "sshd",
                remote: "192.0.2.10",
                session: "7",
              },
            ],
          })
        )
      })
    )
    renderHistory()
    expect(await screen.findByText("Local identity")).toBeTruthy()
    expect(screen.getByText("Login")).toBeTruthy()
    expect(screen.getByText("192.0.2.10")).toBeTruthy()
    expect((screen.getByLabelText("Account") as HTMLInputElement).value).toBe(
      "operator"
    )
  })

  it("shows empty and error states without pretending they are history", async () => {
    let fail = true
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input)
        if (path.endsWith("/auth/session")) {
          return Promise.resolve(
            jsonResponse({
              user: { username: "operator", uid: 1000, gid: 1000 },
              csrfToken: "csrf",
              administrative: true,
            })
          )
        }
        return Promise.resolve(
          fail
            ? new Response("", { status: 503 })
            : jsonResponse({
                identity: {
                  username: "deleted-user",
                  source: "deleted-unknown",
                  present: false,
                },
                items: [],
              })
        )
      })
    )
    renderHistory()
    expect(await screen.findByText("Login history unavailable")).toBeTruthy()

    fail = false
    // A separate render gives the query a fresh retry/cache boundary.
    renderHistory()
    expect(await screen.findByText("No login events")).toBeTruthy()
    expect(screen.getByText("Deleted or unknown identity")).toBeTruthy()
  })
})
