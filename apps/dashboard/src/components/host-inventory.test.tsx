import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { HostInventory } from "@/components/host-inventory"
import type { HostInfo } from "@/lib/api"

const host: HostInfo = {
  hostname: "tako",
  operatingSystem: "Ubuntu",
  kernel: "6.8.0",
  architecture: "amd64",
  uptimeSeconds: 60,
  bootedAt: "2026-08-13T00:00:00Z",
  bootId: "boot-123",
  hardware: {
    available: true,
    cpuModel: "Test CPU",
    cpuCores: 4,
    memoryTotal: 8 * 1024 * 1024 * 1024,
    memoryAvailable: 4 * 1024 * 1024 * 1024,
  },
  shutdown: { available: true, clean: true },
  restart: {
    available: true,
    required: true,
    source: "reboot-required",
    reason: "Kernel updates are pending.",
  },
}

afterEach(() => document.documentElement.classList.remove("dark"))

describe("HostInventory", () => {
  it("shows hardware, shutdown, and restart status", () => {
    render(<HostInventory host={host} />)
    expect(screen.getByText("Test CPU")).toBeTruthy()
    expect(screen.getByText("Clean")).toBeTruthy()
    expect(screen.getByText("Restart required")).toBeTruthy()
    expect(screen.getByText("Kernel updates are pending.")).toBeTruthy()
  })

  it("explains unavailable optional sources responsively in dark mode", () => {
    document.documentElement.classList.add("dark")
    const unavailable: HostInfo = {
      ...host,
      hardware: { available: false, reason: "procfs is unavailable" },
      shutdown: { available: false, clean: false, reason: "No journal" },
      restart: { available: false, required: false, reason: "No provider" },
    }
    const { container } = render(<HostInventory host={unavailable} />)
    expect(screen.getByText("procfs is unavailable")).toBeTruthy()
    expect(screen.getByText("No journal")).toBeTruthy()
    expect(screen.getByText("No provider")).toBeTruthy()
    expect(document.documentElement.classList.contains("dark")).toBe(true)
    expect(container.querySelector("section")?.className).toContain(
      "@4xl/main:grid-cols-3"
    )
  })
})
