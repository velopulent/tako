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
    disks: { nvme0n1: { read: 300, write: 400 } },
    interfaces: {},
  },
]

describe("directional page metrics", () => {
  it("shows only the storage directions", () => {
    render(<StorageMetrics samples={samples} interval="off" />)
    expect(screen.getByText("Disk reads")).toBeTruthy()
    expect(screen.getByText("Disk writes")).toBeTruthy()
    expect(screen.queryByText("Received")).toBeNull()
    expect(screen.queryByText("CPU")).toBeNull()
  })

  it("shows only the network directions", () => {
    render(<NetworkMetrics samples={samples} interval="off" />)
    expect(screen.getByText("Received")).toBeTruthy()
    expect(screen.getByText("Sent")).toBeTruthy()
    expect(screen.queryByText("Disk reads")).toBeNull()
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
