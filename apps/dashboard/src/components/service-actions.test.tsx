import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import type { ComponentProps } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ServiceActions } from "@/components/service-actions"

function renderActions(props: ComponentProps<typeof ServiceActions>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ServiceActions {...props} />
    </QueryClientProvider>
  )
}

afterEach(() => vi.unstubAllGlobals())

describe("ServiceActions", () => {
  it("requires administrative access for system units", () => {
    renderActions({
      scope: "system",
      unit: "demo.service",
      csrfToken: "csrf",
      administrative: false,
      pending: false,
      onAction: vi.fn(),
    })
    expect(
      (screen.getByRole("button", { name: "Restart" }) as HTMLButtonElement)
        .disabled
    ).toBe(true)
  })

  it("confirms a user action before invoking the callback", async () => {
    const onAction = vi.fn()
    const user = userEvent.setup()
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          scope: "user",
          unit: "demo.service",
          action: "restart",
          currentState: "active",
          currentSubState: "running",
          affected: [{ name: "web.service", relationship: "wanted-by" }],
          warnings: ["Review affected relationships before confirming."],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } }
      )
    )
    vi.stubGlobal("fetch", fetchMock)
    renderActions({
      scope: "user",
      unit: "demo.service",
      csrfToken: "csrf",
      administrative: false,
      pending: false,
      onAction,
    })
    await user.click(screen.getByRole("button", { name: "Restart" }))
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    expect(
      await screen.findByText(
        (_, element) =>
          element?.textContent === "Current state: active / running"
      )
    ).toBeTruthy()
    await user.click(screen.getByRole("button", { name: "Confirm" }))
    expect(onAction).toHaveBeenCalledWith("restart")
  })
})
