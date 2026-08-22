import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { ResourceHistoryCharts } from "@/components/process-resource-charts"
import type { ProcessResourceSample } from "@/lib/api"

const sample = (
  overrides: Partial<ProcessResourceSample>
): ProcessResourceSample => ({
  timestamp: "2026-08-13T00:00:00Z",
  cpuTime: 1,
  memory: 1024,
  virtualMemory: 2048,
  diskRead: 10,
  diskWrite: 20,
  ...overrides,
})

describe("ResourceHistoryCharts", () => {
  it("shows the empty state before any samples exist", () => {
    render(<ResourceHistoryCharts history={[]} />)
    expect(screen.getByText("No history yet")).toBeTruthy()
  })

  it("renders both charts when I/O counters are readable", () => {
    render(
      <ResourceHistoryCharts
        history={[
          sample({ diskRead: 10 }),
          sample({
            timestamp: "2026-08-13T00:00:10Z",
            cpuTime: 2,
            diskRead: 110,
            diskWrite: 220,
          }),
        ]}
      />
    )
    expect(screen.getByText("CPU & Memory")).toBeTruthy()
    expect(screen.getByText("Disk I/O throughput")).toBeTruthy()
    expect(screen.queryByText("Disk I/O unavailable")).toBeNull()
  })

  it("replaces the disk chart with a restricted panel when counters are denied", () => {
    Element.prototype.getAnimations = () => [] as Animation[]
    render(
      <ResourceHistoryCharts
        history={[
          sample({ diskRead: 10 }),
          sample({
            timestamp: "2026-08-13T00:00:10Z",
            cpuTime: 2,
            ioDenied: true,
          }),
        ]}
      />
    )
    expect(screen.getByText("CPU & Memory")).toBeTruthy()
    expect(screen.getByText("Disk I/O unavailable")).toBeTruthy()
    expect(
      screen.getByText(/cannot read this process's I\/O counters/)
    ).toBeTruthy()
  })
})
