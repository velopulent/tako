import { describe, expect, it } from "vitest"

import {
  buildAdvisoryGroups,
  formatRelative,
  referenceLabel,
  summarizeSeverity,
  versionDiff,
} from "@/lib/update-advisories"

describe("update advisories", () => {
  it("splits versions at the last separator before they diverge", () => {
    expect(versionDiff("6.7.4-1.fc44", "6.7.5-1.fc44")).toEqual({
      common: "6.7.",
      from: "4-1.fc44",
      to: "5-1.fc44",
    })
    expect(versionDiff(undefined, "1.0")).toEqual({
      common: "",
      from: "",
      to: "1.0",
    })
  })

  it("derives readable titles from advisory source NVRs", () => {
    const groups = buildAdvisoryGroups([
      {
        name: "kf6-kio-core",
        candidateVersion: "6.30.0-1.fc44",
        groupKey: "A",
        summary: "kf6-kio-6.30.0-1.fc44",
      },
      {
        name: "kf6-kio-gui",
        candidateVersion: "6.30.0-1.fc44",
        groupKey: "A",
        summary: "kf6-kio-6.30.0-1.fc44",
      },
    ])
    expect(groups[0].title).toBe("kf6-kio")
  })

  it("reports unclassified inventories without inventing severities", () => {
    const summary = summarizeSeverity([
      { name: "a", candidateVersion: "1" },
      { name: "b", candidateVersion: "1" },
    ])
    expect(summary.classified).toBe(false)
    expect(summary.other).toBe(2)
    expect(summary.enhancement).toBe(0)
  })

  it("labels references compactly", () => {
    expect(referenceLabel("https://www.cve.org/CVERecord?id=CVE-2026-1")).toBe(
      "CVE-2026-1"
    )
    expect(
      referenceLabel("https://bugzilla.redhat.com/show_bug.cgi?id=42")
    ).toBe("#42")
    expect(
      referenceLabel("https://bodhi.fedoraproject.org/updates/FEDORA-2026-x")
    ).toBe("FEDORA-2026-x")
  })

  it("formats relative times", () => {
    const now = Date.parse("2026-10-12T12:00:00Z")
    expect(formatRelative("2026-10-12T11:59:40Z", now)).toBe("just now")
    expect(formatRelative("2026-10-10T12:00:00Z", now)).toMatch(/2 days ago/)
    expect(formatRelative("", now)).toBe("")
  })
})
