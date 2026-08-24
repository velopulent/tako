import * as React from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { SiteHeader } from "@/components/site-header"
import { SidebarProvider } from "@/components/ui/sidebar"
import { pageTitle } from "@/lib/page-title"

vi.mock("@tanstack/react-router", () => ({
  useLocation: () => ({ pathname: "/files" }),
}))
vi.mock("@/components/theme-provider", () => ({
  useTheme: () => ({ theme: "system", setTheme: vi.fn() }),
}))

afterEach(() => vi.unstubAllGlobals())

describe("pageTitle", () => {
  it("shows one top-level page name for nested routes", () => {
    expect(pageTitle("/services/system/sshd.service")).toBe("Services")
    expect(pageTitle("/storage")).toBe("Storage")
    expect(pageTitle("/")).toBe("Dashboard")
  })

  it("falls back safely for an unknown route", () => {
    expect(pageTitle("/unknown")).toBe("Tako")
  })

  it("renders one current page title without a duplicate breadcrumb", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              administrative: false,
              until: "",
              idleTimeoutSeconds: 300,
            }),
            { status: 200, headers: { "Content-Type": "application/json" } }
          )
        )
      )
    )
    vi.stubGlobal("matchMedia", () => ({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    }))
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      React.createElement(
        QueryClientProvider,
        { client },
        React.createElement(
          SidebarProvider,
          null,
          React.createElement(SiteHeader, {
            user: {
              username: "operator",
              name: "Operator",
              uid: 1000,
              gid: 1000,
            },
            csrfToken: "csrf",
          })
        )
      )
    )
    expect((await screen.findAllByText("Files")).length).toBe(1)
    expect(screen.queryByText("Breadcrumb")).toBeNull()
  })
})
