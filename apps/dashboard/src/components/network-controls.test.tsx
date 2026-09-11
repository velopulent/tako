import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { NetworkControls } from "./network-controls"

function jsonResponse(value: unknown) {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  })
}

function renderControls() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <NetworkControls />
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe("NetworkControls", () => {
  it("disables mutations while ownership is conflicted", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        if (String(input).endsWith("/auth/session")) {
          return Promise.resolve(jsonResponse({ csrfToken: "csrf-token" }))
        }
        return Promise.resolve(
          jsonResponse({
            items: [],
            fingerprint: "fingerprint",
            ownership: {
              activeOwner: "",
              detected: ["NetworkManager", "systemd-networkd"],
              conflicted: true,
              reason: "Multiple active network owners were detected.",
            },
          })
        )
      })
    )

    const user = userEvent.setup()
    renderControls()
    await user.type(await screen.findByLabelText("Interface"), "eno1")
    await user.type(
      screen.getByLabelText("Confirmation"),
      "CONFIRM NETWORK CHANGE"
    )

    const applyButton = screen.getByRole("button", {
      name: "Apply Use DHCP",
    }) as HTMLButtonElement
    expect(applyButton.disabled).toBe(true)
    expect(screen.getByText("Ownership conflict")).toBeTruthy()
  })
})
