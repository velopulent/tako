import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ServiceOverride } from "@/components/service-override"

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function renderOverride(
  administrative = false,
  scope: "system" | "user" = "user"
) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ServiceOverride
        scope={scope}
        unit="worker.service"
        csrfToken="csrf-token"
        administrative={administrative}
      />
    </QueryClientProvider>
  )
}

afterEach(() => vi.unstubAllGlobals())

describe("ServiceOverride", () => {
  it("previews the allowlisted drop-in and exposes recovery guidance", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        void input
        void init
        return jsonResponse({
          scope: "user",
          unit: "worker.service",
          path: "/home/octopus/.config/systemd/user/worker.service.d/50-tako.conf",
          exists: true,
          fingerprint: "a".repeat(64),
          guidance: ["Inspect the journal before retrying."],
        })
      }
    )
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    renderOverride()
    await user.click(screen.getByRole("button", { name: "Preview" }))
    expect(await screen.findByText("Managed drop-in found")).toBeTruthy()
    expect(screen.getByText(/Inspect the journal/)).toBeTruthy()
    const call = fetchMock.mock.calls[0]
    expect(String(call?.[0])).toContain("/overrides/preview")
    expect(JSON.parse(String(call?.[1]?.body))).toMatchObject({
      action: "preview",
      unit: "worker.service",
    })
  })

  it("renders system scope as read-only without administrative access", () => {
    renderOverride(false, "system")
    expect(screen.getByText("Administrative access required")).toBeTruthy()
    expect(
      screen
        .getByRole("button", { name: "Apply override" })
        .hasAttribute("disabled")
    ).toBe(true)
  })
})
