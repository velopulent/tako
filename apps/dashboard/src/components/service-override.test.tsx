import { screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ServiceOverride } from "@/components/service-override"
import { jsonResponse, renderWithProviders } from "@/test/render"

const existing = {
  scope: "user",
  unit: "worker.service",
  path: "/home/octopus/.config/systemd/user/worker.service.d/50-tako.conf",
  exists: true,
  fingerprint: "a".repeat(64),
  definition: { environment: { MODE: "prod" }, restart: "on-failure" },
}

afterEach(() => vi.unstubAllGlobals())

describe("ServiceOverride", () => {
  it("loads the current drop-in and saves it with its fingerprint", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse(existing)
    )
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    renderWithProviders(<ServiceOverride scope="user" unit="worker.service" />)
    expect(await screen.findByDisplayValue("MODE")).toBeTruthy()
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({
      action: "preview",
      scope: "user",
      unit: "worker.service",
    })

    await user.type(screen.getAllByLabelText("Variable value")[0], "-2")
    await user.click(screen.getByRole("button", { name: "Save override" }))
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const [url, init] = fetchMock.mock.calls[1]
    expect(String(url)).toBe("/api/v1/services/user/worker.service/overrides")
    expect(JSON.parse(String(init?.body))).toMatchObject({
      action: "apply",
      environment: { MODE: "prod-2" },
      restart: "on-failure",
      expectedFingerprint: "a".repeat(64),
    })
  })

  it("asks for administrative access before reading a system override", () => {
    const fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)
    renderWithProviders(
      <ServiceOverride scope="system" unit="worker.service" />
    )
    expect(screen.getByText("Administrative access required")).toBeTruthy()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
