import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { FirewallControls } from "./firewall-controls"
import { NetworkControls } from "./network-controls"
import { SecurityControls } from "./security-controls"

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function renderControls() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <SecurityControls />
      <NetworkControls />
      <FirewallControls />
    </QueryClientProvider>
  )
}

afterEach(() => vi.unstubAllGlobals())

describe("security and network selectors", () => {
  it("uses shadcn controls without native selects or checkboxes", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input)
        if (path.endsWith("/auth/session")) {
          return Promise.resolve(
            jsonResponse({
              user: { username: "test", name: "Test", uid: 1000, gid: 1000 },
              csrfToken: "csrf",
            })
          )
        }
        if (path.endsWith("/security")) {
          return Promise.resolve(
            jsonResponse({
              active: "SELinux",
              selinux: { mode: "Enforcing" },
              apparmor: { profiles: ["usr.sbin.demo"] },
              findings: [],
              fingerprint: "a".repeat(64),
            })
          )
        }
        if (path.endsWith("/network")) {
          return Promise.resolve(
            jsonResponse({ items: [], fingerprint: "b".repeat(64) })
          )
        }
        return Promise.resolve(
          jsonResponse({
            backend: "firewalld",
            active: true,
            zones: ["public"],
            defaultZone: "public",
            persistentDefaultZone: "public",
            rules: ["public services: ssh"],
            runtimeRules: ["public services: ssh"],
            persistentRules: ["public services: ssh"],
            synchronized: true,
            conflicted: false,
            readOnly: false,
            fingerprint: "c".repeat(64),
          })
        )
      })
    )

    const { container } = renderControls()

    expect(
      await screen.findByRole("combobox", { name: "Framework" })
    ).toBeTruthy()
    expect(container.querySelectorAll("select")).toHaveLength(0)
    expect(container.querySelectorAll('[data-slot="checkbox"]')).toHaveLength(2)
    expect(
      screen.getByRole("checkbox", { name: /Enable boolean/i })
    ).toBeTruthy()
    expect(
      screen.getByRole("checkbox", { name: /Persist firewalld/i })
    ).toBeTruthy()
  })

  it("supports keyboard selection and resets dependent security action", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input)
        if (path.endsWith("/auth/session")) {
          return Promise.resolve(
            jsonResponse({
              user: { username: "test", name: "Test", uid: 1000, gid: 1000 },
              csrfToken: "csrf",
            })
          )
        }
        return Promise.resolve(
          jsonResponse({
            active: "SELinux",
            selinux: { mode: "Enforcing" },
            apparmor: { profiles: ["usr.sbin.demo"] },
            findings: [],
            fingerprint: "a".repeat(64),
          })
        )
      })
    )

    const user = userEvent.setup()
    render(
      <QueryClientProvider
        client={
          new QueryClient({
            defaultOptions: { queries: { retry: false } },
          })
        }
      >
        <SecurityControls />
      </QueryClientProvider>
    )

    const framework = await screen.findByRole("combobox", {
      name: "Framework",
    })
    framework.focus()
    await user.keyboard("{Enter}")
    await user.keyboard("AppArmor")
    await user.keyboard("{Enter}")

    expect(
      screen.getByRole("combobox", { name: "Remediation" }).textContent
    ).toContain("Enforce AppArmor profile")
    expect(
      screen.getByRole("combobox", { name: "AppArmor profile" })
    ).toBeTruthy()
  })
})
