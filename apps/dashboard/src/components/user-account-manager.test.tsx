import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { UserAccountManager } from "@/components/user-account-manager"

const user = {
  username: "operator",
  uid: 1000,
  gid: 1000,
  name: "Operator",
  home: "/home/operator",
  shell: "/bin/bash",
  system: false,
  groups: ["users"],
  source: "local" as const,
  local: true,
  mutable: true,
}

function renderManager() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <UserAccountManager
        user={user}
        csrfToken="csrf"
        administrative
        onApplied={vi.fn()}
      />
    </QueryClientProvider>
  )
}

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

afterEach(() => vi.unstubAllGlobals())

describe("UserAccountManager", () => {
  it("previews before applying a fingerprinted account update", async () => {
    const calls: RequestInit[] = []
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(init ?? {})
        if (String(input).endsWith("/accounts/users/account/preview")) {
          return Promise.resolve(
            jsonResponse({
              action: "update",
              username: "operator",
              current: {
                username: "operator",
                exists: true,
                locked: false,
                fingerprint: "a".repeat(64),
                source: "local",
                user,
              },
              changes: ["display name"],
              warnings: [],
              stale: false,
              allowed: true,
              requiresConfirmation: true,
            })
          )
        }
        return Promise.resolve(
          jsonResponse({
            username: "operator",
            exists: true,
            locked: false,
            fingerprint: "b".repeat(64),
            source: "local",
            user,
          })
        )
      })
    )
    const view = renderManager()
    const client = userEvent.setup()
    await client.click(screen.getByRole("button", { name: "Preview" }))
    expect(await screen.findByText("Account preview")).toBeTruthy()
    await client.click(
      screen.getByRole("button", { name: "Apply account change" })
    )
    await vi.waitFor(() => expect(calls.length).toBe(2))
    expect(JSON.parse(String(calls[1].body))).toMatchObject({
      action: "update",
      username: "operator",
      expectedFingerprint: "a".repeat(64),
    })
    view.unmount()
  })

  it("requires typed confirmation for delete", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve(
          jsonResponse({
            action: "delete",
            username: "operator",
            current: {
              username: "operator",
              exists: true,
              locked: false,
              fingerprint: "a".repeat(64),
              source: "local",
              user,
            },
            changes: ["delete local user", "remove home directory"],
            warnings: ["permanent"],
            stale: false,
            allowed: true,
            requiresConfirmation: true,
          })
        )
      )
    )
    renderManager()
    const client = userEvent.setup()
    await client.click(screen.getByRole("button", { name: "Delete" }))
    const apply = screen.getByRole("button", { name: "Apply account change" })
    expect(apply.hasAttribute("disabled")).toBe(true)
    await client.click(screen.getByRole("button", { name: "Preview" }))
    await screen.findByText("Account preview")
    await client.type(
      screen.getByLabelText("Type DELETE operator to confirm"),
      "DELETE operator"
    )
    expect(apply.hasAttribute("disabled")).toBe(false)
  })
})
