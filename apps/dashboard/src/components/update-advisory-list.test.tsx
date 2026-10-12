import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"

import { UpdateAdvisoryList } from "@/components/update-advisory-list"
import type { UpdatePackage } from "@/lib/api"

const packages: UpdatePackage[] = [
  {
    name: "openssl",
    architecture: "x86_64",
    currentVersion: "3.0.11-1.fc44",
    candidateVersion: "3.0.14-1.fc44",
    severity: "security",
    secSeverity: "important",
    advisoryId: "FEDORA-2026-1",
    groupKey: "FEDORA-2026-1",
    summary: "openssl-3.0.14-1.fc44",
    cveUrls: ["https://www.cve.org/CVERecord?id=CVE-2026-1234"],
    bugUrls: ["https://bugzilla.redhat.com/show_bug.cgi?id=123"],
    description: "A security fix with advisory details.",
    markdown: true,
    dependencies: ["libssl3"],
  },
  {
    name: "openssl-libs",
    architecture: "x86_64",
    currentVersion: "3.0.11-1.fc44",
    candidateVersion: "3.0.14-1.fc44",
    severity: "security",
    advisoryId: "FEDORA-2026-1",
    groupKey: "FEDORA-2026-1",
    summary: "openssl-3.0.14-1.fc44",
  },
  {
    name: "curl",
    architecture: "x86_64",
    currentVersion: "8.5.0-1.fc44",
    candidateVersion: "8.5.1-1.fc44",
    severity: "bugfix",
    groupKey: "FEDORA-2026-2",
    advisoryId: "FEDORA-2026-2",
  },
  {
    name: "zlib",
    architecture: "x86_64",
    candidateVersion: "1.3.1-1.fc44",
  },
  {
    name: "zstd",
    architecture: "x86_64",
    candidateVersion: "1.3.1-1.fc44",
  },
]

describe("UpdateAdvisoryList", () => {
  it("groups by advisory only and orders security first", () => {
    render(<UpdateAdvisoryList packages={packages} />)

    const rows = screen.getAllByRole("listitem")
    expect(rows).toHaveLength(4)
    expect(rows[0].textContent).toContain("openssl")
    expect(rows[0].textContent).toContain("2 packages")
    expect(rows[0].textContent).toContain("Important")
    // Unclassified packages sharing a version stay separate rows.
    expect(rows[2].textContent).toContain("zlib")
    expect(rows[3].textContent).toContain("zstd")
  })

  it("expands an advisory with references and per-package versions", async () => {
    const user = userEvent.setup()
    render(<UpdateAdvisoryList packages={packages} />)

    await user.click(screen.getByRole("button", { name: /openssl/ }))

    expect(
      screen.getByText("A security fix with advisory details.")
    ).toBeTruthy()
    expect(screen.getByRole("link", { name: /CVE-2026-1234/ })).toBeTruthy()
    expect(screen.getByRole("link", { name: /#123/ })).toBeTruthy()
    expect(screen.getByText("Pulls in libssl3")).toBeTruthy()
    expect(screen.getByText("openssl-libs")).toBeTruthy()
  })

  it("filters by type and search, with a recoverable empty state", async () => {
    const user = userEvent.setup()
    render(<UpdateAdvisoryList packages={packages} />)

    await user.click(screen.getByRole("button", { name: /Bug fixes/ }))
    expect(screen.getAllByRole("listitem")).toHaveLength(1)

    await user.type(screen.getByRole("searchbox"), "openssl")
    expect(await screen.findByText("No matching updates")).toBeTruthy()

    await user.click(screen.getByRole("button", { name: "Clear filters" }))
    expect(screen.getAllByRole("listitem")).toHaveLength(4)
  })

  it("finds advisories by CVE identifier", async () => {
    const user = userEvent.setup()
    render(<UpdateAdvisoryList packages={packages} />)

    await user.type(screen.getByRole("searchbox"), "cve-2026-1234")
    const rows = await screen.findAllByRole("listitem")
    expect(rows).toHaveLength(1)
    expect(rows[0].textContent).toContain("openssl")
  })
})
