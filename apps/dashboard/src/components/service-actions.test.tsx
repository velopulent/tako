import { screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ServiceActions } from "@/components/service-actions"
import type { ServiceDetail } from "@/lib/api"
import { jsonResponse, renderWithProviders } from "@/test/render"

const unit = (overrides: Partial<ServiceDetail>) =>
  ({
    name: "demo.service",
    description: "Demo",
    scope: "system",
    type: "service",
    loadState: "loaded",
    activeState: "active",
    subState: "running",
    fileState: "enabled",
    ...overrides,
  }) as ServiceDetail

afterEach(() => vi.unstubAllGlobals())

describe("ServiceActions", () => {
  it("disables system unit actions without administrative access", () => {
    renderWithProviders(<ServiceActions unit={unit({ scope: "system" })} />)
    expect(
      (screen.getByRole("button", { name: "Restart" }) as HTMLButtonElement)
        .disabled
    ).toBe(true)
    expect(
      (screen.getByRole("button", { name: "Stop" }) as HTMLButtonElement)
        .disabled
    ).toBe(true)
  })

  it("lets a user manage their own units", () => {
    renderWithProviders(<ServiceActions unit={unit({ scope: "user" })} />)
    expect(
      (screen.getByRole("button", { name: "Restart" }) as HTMLButtonElement)
        .disabled
    ).toBe(false)
  })

  it("starts a stopped unit without confirmation", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse(unit({ activeState: "active" })))
    vi.stubGlobal("fetch", fetchMock)
    renderWithProviders(
      <ServiceActions
        unit={unit({ activeState: "inactive", subState: "dead" })}
      />,
      { administrative: true }
    )
    await userEvent.setup().click(screen.getByRole("button", { name: "Start" }))
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe("/api/v1/services/system/demo.service/actions")
    expect(JSON.parse(String(init.body))).toEqual({ action: "start" })
    expect(
      (init.headers as Record<string, string>)["X-CSRF-Token"]
    ).toBeUndefined()
  })

  it("previews the impact before a disruptive restart", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) =>
      Promise.resolve(
        String(input).endsWith("/preview")
          ? jsonResponse({
              scope: "system",
              unit: "demo.service",
              action: "restart",
              currentState: "active",
              currentSubState: "running",
              affected: [{ name: "web.service", relationship: "wanted-by" }],
              warnings: [],
            })
          : jsonResponse(unit({}))
      )
    )
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    renderWithProviders(<ServiceActions unit={unit({})} />, {
      administrative: true,
    })
    await user.click(screen.getByRole("button", { name: "Restart" }))
    expect(await screen.findByText("web.service")).toBeTruthy()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await user.click(
      screen.getByRole("button", { name: "Restart", hidden: false })
    )
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(String(fetchMock.mock.calls[1][0])).toBe(
      "/api/v1/services/system/demo.service/actions"
    )
  })
})
