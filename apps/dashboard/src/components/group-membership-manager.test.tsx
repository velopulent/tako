import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { GroupMembershipManager } from "@/components/group-membership-manager"

const group = {
  name: "developers",
  gid: 2000,
  members: [],
  source: "local" as const,
  local: true,
  mutable: true,
}

const users = [
  {
    username: "target",
    uid: 1000,
    gid: 1000,
    name: "Target",
    home: "/home/target",
    shell: "/bin/bash",
    system: false,
    groups: [],
    source: "local" as const,
    local: true,
    mutable: true,
  },
]

function renderManager() {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <GroupMembershipManager
        group={group}
        users={users}
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

describe("GroupMembershipManager", () => {
  it("previews a local membership change before apply", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      void input
      return Promise.resolve(
        jsonResponse({
          action: "add",
          username: "target",
          group: "developers",
          current: {
            username: "target",
            group,
            member: false,
            fingerprint: "a".repeat(64),
          },
          changes: ["add user to local group"],
          warnings: [],
          stale: false,
          allowed: true,
          requiresConfirmation: true,
        })
      )
    })
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    renderManager()
    await user.click(screen.getByRole("combobox", { name: "Local user" }))
    await user.click(await screen.findByRole("option", { name: "target" }))
    await user.click(screen.getByRole("button", { name: "Preview" }))
    expect(await screen.findByText("Membership preview")).toBeTruthy()
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain(
      "/accounts/groups/membership/preview"
    )
  })
})
