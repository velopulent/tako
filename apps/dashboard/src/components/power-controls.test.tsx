import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { PowerControls } from "@/components/power-controls"
import type { PowerStatus } from "@/lib/api"

const status: PowerStatus = {
  available: true,
  reboot: { state: "available", available: true },
  shutdown: { state: "available", available: true },
  inhibitors: [
    {
      what: "shutdown",
      who: "backup",
      why: "Snapshot in progress",
      mode: "block",
      uid: 1000,
      pid: 42,
    },
  ],
  fingerprint: "a".repeat(64),
}

function renderControls(value = status, administrative = true) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <PowerControls
        status={value}
        csrfToken="csrf-token"
        administrative={administrative}
      />
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.documentElement.classList.remove("dark")
})

describe("PowerControls", () => {
  it("shows inhibitors and keeps actions read-only without administrative access", () => {
    document.documentElement.classList.add("dark")
    renderControls(status, false)
    expect(screen.getByText("Active inhibitors")).toBeTruthy()
    expect(screen.getByText(/Snapshot in progress/)).toBeTruthy()
    expect(
      (screen.getByRole("button", { name: "Reboot" }) as HTMLButtonElement)
        .disabled
    ).toBe(true)
    expect(document.documentElement.classList.contains("dark")).toBe(true)
  })

  it("requires typed confirmation before sending a power request", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ action: "reboot", message: "accepted" }), {
        status: 202,
        headers: { "Content-Type": "application/json" },
      })
    )
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    renderControls()
    await user.click(screen.getByRole("button", { name: "Reboot" }))
    const confirm = screen.getByLabelText("Confirmation")
    expect(
      (
        screen.getByRole("button", {
          name: "Confirm reboot",
        }) as HTMLButtonElement
      ).disabled
    ).toBe(true)
    await user.type(confirm, "REBOOT")
    expect(
      (
        screen.getByRole("button", {
          name: "Confirm reboot",
        }) as HTMLButtonElement
      ).disabled
    ).toBe(false)
    await user.click(screen.getByRole("button", { name: "Confirm reboot" }))
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    expect(fetchMock.mock.calls[0]?.[1]?.body).toBe(
      JSON.stringify({
        action: "reboot",
        confirmation: "REBOOT",
        expectedFingerprint: status.fingerprint,
      })
    )
  })
})
