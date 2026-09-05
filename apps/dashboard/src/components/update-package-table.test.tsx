import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"

import { UpdatePackageTable } from "@/components/update-package-table"
import type { UpdatePackage } from "@/lib/api"

const packages: UpdatePackage[] = [
  {
    name: "openssl",
    architecture: "amd64",
    currentVersion: "3.0.11",
    candidateVersion: "3.0.14",
    severity: "security",
    secSeverity: "important",
    advisoryId: "CVE-2026-1234",
    groupKey: "security-1",
    cveUrls: ["https://security.example/CVE-2026-1234"],
    vendorUrls: ["https://security.example/errata-1"],
    bugUrls: ["https://bugs.example/123"],
    description: "A security fix with advisory details.",
    markdown: true,
    dependencies: ["libssl3"],
  },
  {
    name: "libssl3",
    architecture: "amd64",
    currentVersion: "3.0.11",
    candidateVersion: "3.0.14",
    severity: "security",
    advisoryId: "CVE-2026-1234",
    groupKey: "security-1",
    description: "A security fix with advisory details.",
    markdown: true,
  },
  {
    name: "curl",
    architecture: "amd64",
    currentVersion: "8.5.0",
    candidateVersion: "8.5.1",
    severity: "bugfix",
    groupKey: "bugfix-1",
    bugUrls: ["https://bugs.example/456"],
    summary: "A maintenance fix.",
  },
]

describe("UpdatePackageTable", () => {
  it("groups packages and exposes responsive desktop and mobile markup", () => {
    const { container } = render(<UpdatePackageTable packages={packages} />)

    expect(container.querySelector('[data-slot="table"]')).toBeTruthy()
    expect(container.querySelector('[data-slot="card"]')).toBeTruthy()
    expect(
      screen.getAllByText("libssl3 (amd64), openssl (amd64)")
    ).toHaveLength(2)
    expect(
      screen.getAllByRole("button", { name: /Expand advisory details/ })
    ).toHaveLength(4)
  })

  it("expands advisory details with links and dependency information", async () => {
    const user = userEvent.setup()
    render(<UpdatePackageTable packages={packages} />)

    await user.click(
      screen.getAllByRole("button", { name: /Expand advisory details/ })[0]
    )

    expect(screen.getAllByText("Dependencies: libssl3")).toHaveLength(2)
    expect(screen.getAllByRole("link", { name: "CVE-2026-1234" })).toHaveLength(
      2
    )
    expect(screen.getAllByRole("link", { name: "errata-1" })).toHaveLength(2)
    expect(screen.getAllByRole("link", { name: "123" })).toHaveLength(2)
    expect(screen.getAllByText("3.0.11 → 3.0.14")).toHaveLength(6)
  })
})
