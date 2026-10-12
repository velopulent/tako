import type { UpdatePackage } from "@/lib/api"

export type UpdateSeverity = "security" | "bugfix" | "enhancement" | "other"
export type SecuritySeverity = NonNullable<UpdatePackage["secSeverity"]>

export type AdvisoryGroup = {
  key: string
  advisoryId?: string
  title: string
  packages: UpdatePackage[]
  severity: UpdateSeverity
  secSeverity?: SecuritySeverity
  description: string
  markdown: boolean
  issued?: string
  cveUrls: string[]
  bugUrls: string[]
  vendorUrls: string[]
}

const severityOrder: Record<UpdateSeverity, number> = {
  security: 0,
  bugfix: 1,
  enhancement: 2,
  other: 3,
}

const securityOrder: Record<SecuritySeverity, number> = {
  critical: 0,
  important: 1,
  moderate: 2,
  low: 3,
}

export function packageSeverity(pkg: UpdatePackage): UpdateSeverity {
  const value = (pkg.severity ?? "").toLowerCase()
  if (value === "security" || pkg.secSeverity) return "security"
  if (value === "bugfix") return "bugfix"
  if (value === "enhancement") return "enhancement"
  return "other"
}

export function packageLabel(pkg: UpdatePackage) {
  return pkg.architecture ? `${pkg.name}.${pkg.architecture}` : pkg.name
}

function unique<T>(values: T[]) {
  return Array.from(new Set(values))
}

// Advisory titles are source NVRs ("kf6-6.30.0-1.fc44, plasma-6.7.5-1.fc44").
// Dropping the version-release leaves a readable list of source projects.
function titleFromAdvisory(summary: string | undefined) {
  if (!summary) return ""
  const names = unique(
    summary
      .split(/,\s*/)
      .map((part) => part.trim().replace(/-[^-\s]+-[^-\s]+$/, ""))
      .filter(Boolean)
  )
  if (names.length <= 2) return names.join(", ")
  return `${names.slice(0, 2).join(", ")} +${names.length - 2} more`
}

export function buildAdvisoryGroups(packages: UpdatePackage[]) {
  const map = new Map<string, UpdatePackage[]>()
  for (const pkg of packages) {
    // Only vendor advisories group packages. Guessing by version string merged
    // unrelated packages into unreadable walls of names.
    const key =
      pkg.groupKey?.trim() ||
      pkg.advisoryId?.trim() ||
      `pkg:${packageLabel(pkg)}`
    const list = map.get(key)
    if (list) list.push(pkg)
    else map.set(key, [pkg])
  }

  const groups: AdvisoryGroup[] = []
  for (const [key, list] of map) {
    list.sort((a, b) => a.name.localeCompare(b.name))
    const severity = list
      .map(packageSeverity)
      .sort((a, b) => severityOrder[a] - severityOrder[b])[0]
    const secSeverity = list
      .flatMap((pkg) => (pkg.secSeverity ? [pkg.secSeverity] : []))
      .sort((a, b) => securityOrder[a] - securityOrder[b])[0]
    const lead = list.find((pkg) => pkg.description || pkg.details) ?? list[0]
    const advisoryId = list.find((pkg) => pkg.advisoryId)?.advisoryId
    groups.push({
      key,
      advisoryId,
      title:
        list.length === 1
          ? list[0].name
          : titleFromAdvisory(list.find((pkg) => pkg.summary)?.summary) ||
            list[0].name,
      packages: list,
      severity,
      secSeverity,
      description: lead.description || lead.details || "",
      markdown: list.some((pkg) => pkg.markdown),
      issued: list.find((pkg) => pkg.issued)?.issued,
      cveUrls: unique(list.flatMap((pkg) => pkg.cveUrls ?? [])),
      bugUrls: unique(list.flatMap((pkg) => pkg.bugUrls ?? [])),
      vendorUrls: unique(list.flatMap((pkg) => pkg.vendorUrls ?? [])),
    })
  }

  return groups.sort((a, b) => {
    const bySeverity = severityOrder[a.severity] - severityOrder[b.severity]
    if (bySeverity) return bySeverity
    const bySecurity =
      (a.secSeverity ? securityOrder[a.secSeverity] : 4) -
      (b.secSeverity ? securityOrder[b.secSeverity] : 4)
    if (bySecurity) return bySecurity
    return a.title.localeCompare(b.title)
  })
}

export type SeveritySummary = Record<UpdateSeverity, number> & {
  critical: number
  classified: boolean
}

// Counts advisories rather than packages so the summary matches the list rows.
export function summarizeSeverity(packages: UpdatePackage[]): SeveritySummary {
  const summary: SeveritySummary = {
    security: 0,
    bugfix: 0,
    enhancement: 0,
    other: 0,
    critical: 0,
    classified: false,
  }
  const groups = buildAdvisoryGroups(packages)
  for (const group of groups) {
    summary[group.severity] += 1
    if (group.secSeverity === "critical") summary.critical += 1
  }
  summary.classified = summary.other < groups.length
  return summary
}

export function groupMatches(group: AdvisoryGroup, query: string) {
  if (!query) return true
  const needle = query.toLowerCase()
  return (
    group.title.toLowerCase().includes(needle) ||
    (group.advisoryId ?? "").toLowerCase().includes(needle) ||
    group.cveUrls.some((url) =>
      referenceLabel(url).toLowerCase().includes(needle)
    ) ||
    group.packages.some((pkg) => pkg.name.toLowerCase().includes(needle))
  )
}

export function referenceLabel(url: string) {
  const cve = url.match(/CVE-\d{4}-\d+/i)?.[0]
  if (cve) return cve.toUpperCase()
  const bug = url.match(/[?&]id=(\d+)$/)?.[1] ?? url.match(/\/(\d+)\/?$/)?.[1]
  if (bug) return `#${bug}`
  const tail = url.replace(/\/$/, "").match(/[^/=]+$/)?.[0]
  return tail ?? url
}

// Splits two versions at the last separator before they diverge, so the
// changed portion can be emphasised: 6.7.4-1.fc44 -> 6.7.5-1.fc44.
export function versionDiff(from: string | undefined, to: string) {
  if (!from || from === to) return { common: "", from: from ?? "", to }
  let index = 0
  while (
    index < from.length &&
    index < to.length &&
    from[index] === to[index]
  ) {
    index += 1
  }
  while (index > 0 && !/[.\-:~^+_]/.test(to[index - 1])) index -= 1
  return {
    common: to.slice(0, index),
    from: from.slice(index),
    to: to.slice(index),
  }
}

const relativeUnits: Array<[Intl.RelativeTimeFormatUnit, number]> = [
  ["year", 365 * 24 * 3600],
  ["month", 30 * 24 * 3600],
  ["week", 7 * 24 * 3600],
  ["day", 24 * 3600],
  ["hour", 3600],
  ["minute", 60],
]

export function formatRelative(
  value: string | number | undefined,
  now = Date.now()
) {
  if (value === undefined || value === "") return ""
  const time = new Date(value).getTime()
  if (Number.isNaN(time)) return ""
  const seconds = Math.round((time - now) / 1000)
  const format = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" })
  for (const [unit, size] of relativeUnits) {
    if (Math.abs(seconds) >= size) {
      return format.format(Math.round(seconds / size), unit)
    }
  }
  return "just now"
}

export function formatAbsolute(value: string | number | undefined) {
  if (value === undefined || value === "") return ""
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ""
  return date.toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  })
}

export function firstLine(text: string) {
  for (const raw of text.split("\n")) {
    const line = raw.replace(/^[-*#=\s]+/, "").trim()
    if (line) return line
  }
  return ""
}

export function plural(count: number, one: string, many = `${one}s`) {
  return `${count.toLocaleString()} ${count === 1 ? one : many}`
}
