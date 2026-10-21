import { fireEvent, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { api, streamURL } from "@/lib/api"
import { useActivityHeartbeat } from "./use-activity-heartbeat"

function stubFetch() {
  const fetch = vi.fn(
    async (_input: RequestInfo | URL, _init?: RequestInit) =>
      new Response(null, { status: 204 })
  )
  vi.stubGlobal("fetch", fetch)
  return fetch
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe("idle-timeout activity", () => {
  it("reports interaction at most once a minute", () => {
    vi.useFakeTimers()
    const fetch = stubFetch()
    renderHook(() => useActivityHeartbeat(true))

    fireEvent.pointerDown(window)
    fireEvent.keyDown(window)
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch.mock.calls[0]?.[0]).toBe("/api/v1/auth/activity")

    vi.advanceTimersByTime(61_000)
    fireEvent.keyDown(window)
    expect(fetch).toHaveBeenCalledTimes(2)
  })

  it("stays quiet before sign-in", () => {
    const fetch = stubFetch()
    renderHook(() => useActivityHeartbeat(false))
    fireEvent.pointerDown(window)
    expect(fetch).not.toHaveBeenCalled()
  })

  it("marks reads and streams as background but not mutations", async () => {
    const fetch = stubFetch()
    await api("/processes")
    await api("/auth/activity", { method: "POST" })
    const headers = (call: number) =>
      (fetch.mock.calls[call]?.[1]?.headers ?? {}) as Record<string, string>
    expect(headers(0)["X-Tako-Background"]).toBe("1")
    expect(headers(1)["X-Tako-Background"]).toBeUndefined()
    expect(streamURL("/api/v1/updates/live")).toBe(
      "/api/v1/updates/live?background=1"
    )
    expect(streamURL("/api/v1/metrics/stream?interval=5s")).toBe(
      "/api/v1/metrics/stream?interval=5s&background=1"
    )
  })
})
