import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { SSHKeyManager } from "@/components/ssh-key-manager"

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
      <SSHKeyManager />
    </QueryClientProvider>
  )
}

const session = {
  user: { username: "operator", uid: 1000, gid: 1000 },
  csrfToken: "csrf",
  administrative: false,
}
const user = {
  username: "operator",
  uid: 1000,
  gid: 1000,
  name: "Operator",
  home: "/home/operator",
  shell: "/bin/bash",
  system: false,
  groups: [],
  source: "local" as const,
  local: true,
  mutable: true,
}

afterEach(() => vi.unstubAllGlobals())

describe("SSHKeyManager", () => {
  it("previews and applies an authorized key without exposing receipt material", async () => {
    const calls: [RequestInfo | URL, RequestInit | undefined][] = []
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        calls.push([input, init])
        const path = String(input)
        if (path.endsWith("/auth/session"))
          return Promise.resolve(jsonResponse(session))
        if (path.endsWith("/users"))
          return Promise.resolve(jsonResponse({ items: [user] }))
        if (path.includes("/users/ssh-keys") && !path.endsWith("/preview")) {
          return Promise.resolve(
            jsonResponse({
              username: "operator",
              path: "/home/operator/.ssh/authorized_keys",
              fingerprint: "a".repeat(64),
              keys: [],
              writable: true,
              authority: "user",
            })
          )
        }
        if (path.endsWith("/preview")) {
          return Promise.resolve(
            jsonResponse({
              action: "add",
              username: "operator",
              current: {
                username: "operator",
                path: "/home/operator/.ssh/authorized_keys",
                fingerprint: "a".repeat(64),
                keys: [],
                writable: true,
                authority: "user",
              },
              changes: ["add key"],
              warnings: [],
              stale: false,
              allowed: true,
              requiresConfirmation: false,
            })
          )
        }
        return Promise.resolve(
          jsonResponse({
            username: "operator",
            path: "/home/operator/.ssh/authorized_keys",
            fingerprint: "b".repeat(64),
            keys: [],
            writable: true,
            authority: "user",
          })
        )
      })
    )
    renderManager()
    const userActions = userEvent.setup()
    await userActions.type(
      await screen.findByLabelText("Public key"),
      "ssh-ed25519 AQID test"
    )
    await userActions.click(screen.getByRole("button", { name: "Preview" }))
    expect(await screen.findByText("Key preview")).toBeTruthy()
    await userActions.click(
      screen.getByRole("button", { name: "Apply key change" })
    )
    await vi.waitFor(() =>
      expect(
        calls.some(([input]) => String(input).endsWith("/users/ssh-keys"))
      ).toBe(true)
    )
    const apply = calls.find(([input]) =>
      String(input).endsWith("/users/ssh-keys")
    )
    expect(JSON.parse(String(apply?.[1]?.body))).toMatchObject({
      action: "add",
      username: "operator",
      key: "ssh-ed25519 AQID test",
      expectedFingerprint: "a".repeat(64),
    })
  })

  it("requires confirmation before removing a key", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input)
        if (path.endsWith("/auth/session"))
          return Promise.resolve(jsonResponse(session))
        if (path.endsWith("/users"))
          return Promise.resolve(jsonResponse({ items: [user] }))
        return Promise.resolve(
          jsonResponse({
            username: "operator",
            path: "/home/operator/.ssh/authorized_keys",
            fingerprint: "a".repeat(64),
            keys: [
              {
                fingerprint: "b".repeat(64),
                type: "ssh-ed25519",
                comment: "laptop",
                line: "ssh-ed25519 AQID laptop",
              },
            ],
            writable: true,
            authority: "user",
          })
        )
      })
    )
    renderManager()
    const userActions = userEvent.setup()
    await userActions.click(
      await screen.findByRole("button", { name: "Remove" })
    )
    expect(screen.getByLabelText(/Type REMOVE KEY/)).toBeTruthy()
    expect(
      (
        screen.getByRole("button", {
          name: "Apply key change",
        }) as HTMLButtonElement
      ).disabled
    ).toBe(true)
  })
})
