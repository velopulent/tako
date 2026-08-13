import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import {
  NetworkMetrics,
  StorageMetrics,
} from "@/components/directional-metrics"

const samples = [
  {
    timestamp: "2026-08-13T00:00:00Z",
    cpuPercent: 42,
    memoryUsed: 4,
    memoryTotal: 8,
    load1: 1,
    load5: 1,
    load15: 1,
    cpuCorePercent: [42],
    swapUsed: 0,
    swapTotal: 0,
    networkRx: 100,
    networkTx: 200,
    diskRead: 300,
    diskWrite: 400,
    interfaces: {},
  },
]

describe("directional page metrics", () => {
  it("shows only separate storage directions", () => {
    render(<StorageMetrics samples={samples} interval="off" />)
    expect(screen.getByText("Storage reads")).toBeTruthy()
    expect(screen.getByText("Storage writes")).toBeTruthy()
    expect(screen.queryByText("CPU utilization")).toBeNull()
    expect(screen.queryByText("Network traffic")).toBeNull()
  })

  it("shows only separate network directions", () => {
    render(<NetworkMetrics samples={samples} interval="off" />)
    expect(screen.getByText("Network receiving")).toBeTruthy()
    expect(screen.getByText("Network transmitting")).toBeTruthy()
    expect(screen.queryByText("CPU utilization")).toBeNull()
    expect(screen.queryByText("Storage I/O")).toBeNull()
  })

  it("keeps both directional cards responsive in dark mode", () => {
    document.documentElement.classList.add("dark")
    const { container } = render(
      <StorageMetrics samples={samples} interval="off" />
    )
    const grid = container.querySelector("section")
    expect(document.documentElement.classList.contains("dark")).toBe(true)
    expect(grid?.classList.contains("grid")).toBe(true)
    expect(grid?.classList.contains("@4xl/main:grid-cols-2")).toBe(true)
    document.documentElement.classList.remove("dark")
  })

  it("distinguishes telemetry loading, failure, and empty states", () => {
    const { rerender } = render(
      <StorageMetrics samples={[]} interval="off" pending />
    )
    expect(screen.getByText("Loading storage telemetry")).toBeTruthy()

    rerender(<StorageMetrics samples={[]} interval="off" error />)
    expect(screen.getByText("Storage telemetry unavailable")).toBeTruthy()

    rerender(<StorageMetrics samples={[]} interval="off" />)
    expect(screen.getByText("No storage telemetry yet")).toBeTruthy()
  })
})
