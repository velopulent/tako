import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { PasswordManager } from "@/components/password-manager"

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function renderManager() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <PasswordManager />
    </QueryClientProvider>
  )
}

afterEach(() => vi.unstubAllGlobals())

describe("PasswordManager", () => {
  it("submits a self-service change without exposing the secret in the UI", async () => {
    const calls: [RequestInfo | URL, RequestInit | undefined][] = []
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        calls.push([input, init])
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
        if (path.endsWith("/users")) {
          return Promise.resolve(jsonResponse({ items: [] }))
        }
        return Promise.resolve(new Response(null, { status: 204 }))
      })
    )
    renderManager()
    const user = userEvent.setup()
    await user.type(
      await screen.findByLabelText("Current password"),
      "old-secret"
    )
    await user.type(screen.getByLabelText("New password"), "new-secret")
    await user.type(screen.getByLabelText("Confirm new password"), "new-secret")
    await user.click(screen.getByRole("button", { name: "Change password" }))
    await screen.findByText("Password updated")
    const request = calls.find(([input]) =>
      String(input).endsWith("/users/password")
    )
    expect(request).toBeTruthy()
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({
      action: "change",
      currentPassword: "old-secret",
      newPassword: "new-secret",
      confirmation: "new-secret",
    })
    expect(document.body.textContent).not.toContain("old-secret")
  })

  it("shows a distinct safe policy error", async () => {
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
        if (path.endsWith("/users"))
          return Promise.resolve(jsonResponse({ items: [] }))
        return Promise.resolve(
          jsonResponse(
            { code: "password-policy-failed", detail: "safe policy detail" },
            422
          )
        )
      })
    )
    renderManager()
    const user = userEvent.setup()
    await user.type(
      await screen.findByLabelText("Current password"),
      "old-secret"
    )
    await user.type(screen.getByLabelText("New password"), "new-secret")
    await user.type(screen.getByLabelText("Confirm new password"), "new-secret")
    await user.click(screen.getByRole("button", { name: "Change password" }))
    expect(
      await screen.findByText(
        "The host password policy rejected the new password."
      )
    ).toBeTruthy()
    expect(document.body.textContent).not.toContain("new-secret")
  })

  it("offers administrative reset only for local users", async () => {
    const requests: [RequestInfo | URL, RequestInit | undefined][] = []
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        requests.push([input, init])
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
        if (path.endsWith("/users")) {
          return Promise.resolve(
            jsonResponse({
              items: [
                {
                  username: "target",
                  uid: 1001,
                  gid: 1001,
                  name: "Target",
                  home: "/home/target",
                  shell: "/bin/bash",
                  system: false,
                  groups: [],
                  source: "local",
                  local: true,
                  mutable: true,
                },
              ],
            })
          )
        }
        return Promise.resolve(new Response(null, { status: 204 }))
      })
    )
    renderManager()
    const user = userEvent.setup()
    await user.click(
      await screen.findByRole("button", { name: "Reset another password" })
    )
    await user.type(screen.getByLabelText("New password"), "reset-secret")
    await user.type(
      screen.getByLabelText("Confirm new password"),
      "reset-secret"
    )
    await user.click(screen.getByRole("button", { name: "Reset password" }))
    await vi.waitFor(() => {
      expect(
        requests.some(([input]) => String(input).endsWith("/users/password"))
      ).toBe(true)
    })
    const request = requests.find(([input]) =>
      String(input).endsWith("/users/password")
    )
    expect(JSON.parse(String(request?.[1]?.body))).toMatchObject({
      action: "reset",
      username: "target",
    })
    expect(document.body.textContent).not.toContain("reset-secret")
  })
})
