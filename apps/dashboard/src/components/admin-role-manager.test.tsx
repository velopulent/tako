import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { AdminRoleManager } from "@/components/admin-role-manager"

afterEach(() => vi.unstubAllGlobals())

describe("AdminRoleManager", () => {
  it("documents the structured role boundary without a sudoers editor", () => {
    const client = new QueryClient()
    render(
      <QueryClientProvider client={client}>
        <AdminRoleManager
          users={[]}
          csrfToken="csrf"
          administrative
          onApplied={vi.fn()}
        />
      </QueryClientProvider>
    )
    expect(screen.getByText(/Sudoers text is never exposed/)).toBeTruthy()
    expect(screen.queryByText(/sudoers file/i)).toBeNull()
    expect(screen.getByRole("button", { name: "Preview" })).toBeTruthy()
  })
})
