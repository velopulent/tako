import * as React from "react"
import {
  Shield,
  Bug,
  Sparkles,
  ChevronRight,
  ChevronDown,
} from "lucide-react"

import type { UpdatePackage } from "@/lib/api"
import { Checkbox } from "@/components/ui/checkbox"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

function getSeverityIcon(severity?: string) {
  const normalized = (severity ?? "").toLowerCase()
  if (normalized === "security") {
    return <Shield className="size-4 text-destructive" aria-hidden />
  }
  if (normalized === "bugfix") {
    return <Bug className="size-4 text-amber-600" aria-hidden />
  }
  return <Sparkles className="size-4 text-blue-600" aria-hidden />
}

function getSeverityLabel(severity?: string) {
  if (severity === "security") return "security"
  if (severity === "bugfix") return "bug fix"
  return "enhancement"
}

function firstLine(text?: string) {
  if (!text) return "-"
  const line = text.trim().split("\n")[0] ?? ""
  const clean = line.replace(/^[-*=\s]+/, "").trim()
  return clean.slice(0, 120) || "-"
}

type AdvisoryGroup = {
  key: string
  packages: UpdatePackage[]
  version: string
  severity: string
  description: string
}

function buildGroups(packages: UpdatePackage[]): AdvisoryGroup[] {
  const map = new Map<string, UpdatePackage[]>()
  for (const pkg of packages) {
    const key = pkg.groupKey?.trim() || pkg.advisoryId?.trim() || `${pkg.candidateVersion}::${pkg.summary ?? ""}::${pkg.severity ?? ""}`
    const list = map.get(key)
    if (list) list.push(pkg)
    else map.set(key, [pkg])
  }
  const groups: AdvisoryGroup[] = []
  for (const [key, pkgs] of map.entries()) {
    pkgs.sort((a, b) => a.name.localeCompare(b.name))
    const severityRank = (s?: string) => (s === "security" ? 3 : s === "bugfix" ? 2 : 1)
    const highest = [...pkgs].sort((a, b) => severityRank(b.severity) - severityRank(a.severity))[0]
    groups.push({
      key,
      packages: pkgs,
      version: pkgs[0]?.candidateVersion ?? "",
      severity: highest?.severity ?? "enhancement",
      description: highest?.description ?? highest?.details ?? highest?.summary ?? "",
    })
  }
  groups.sort((a, b) => {
    const rank = (s: string) => (s === "security" ? 0 : s === "bugfix" ? 1 : 2)
    const diff = rank(a.severity) - rank(b.severity)
    if (diff !== 0) return diff
    return a.packages[0].name.localeCompare(b.packages[0].name)
  })
  return groups
}

export function UpdatePackageTable({
  packages,
  selected,
  onToggle,
  selectable = true,
}: {
  packages: UpdatePackage[]
  selected: string[]
  onToggle: (name: string) => void
  selectable?: boolean
}) {
  const groups = React.useMemo(() => buildGroups(packages), [packages])
  const selectedSet = React.useMemo(() => new Set(selected), [selected])
  const [expanded, setExpanded] = React.useState<Set<string>>(() => new Set())

  const toggleExpanded = (key: string) =>
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })

  const allSelected = groups.length > 0 && groups.every((g) => g.packages.every((p) => selectedSet.has(p.name)))
  const headerChecked = allSelected

  const handleGroupToggle = (group: AdvisoryGroup) => {
    const every = group.packages.every((p) => selectedSet.has(p.name))
    // toggle whole group atomically: if every selected -> deselect all, else select all (including dependencies)
    if (every) {
      for (const pkg of group.packages) {
        if (selectedSet.has(pkg.name)) onToggle(pkg.name)
      }
    } else {
      for (const pkg of group.packages) {
        if (!selectedSet.has(pkg.name)) onToggle(pkg.name)
      }
    }
  }

  const handleHeaderToggle = () => {
    if (allSelected) {
      groups.forEach((g) => g.packages.forEach((p) => selectedSet.has(p.name) && onToggle(p.name)))
    } else {
      groups.forEach((g) => g.packages.forEach((p) => !selectedSet.has(p.name) && onToggle(p.name)))
    }
  }

  return (
    <div className="overflow-hidden rounded-md border">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[52rem] text-sm">
          <thead className="bg-muted/50 text-left">
            <tr>
              <th className="w-8 px-2 py-2">
                <span className="sr-only">Expand</span>
              </th>
              {selectable && (
                <th className="w-10 px-2 py-2">
                  <Checkbox
                    aria-label="Select all"
                    checked={headerChecked}
                    onCheckedChange={handleHeaderToggle}
                  />
                </th>
              )}
              <th className="px-3 py-2 font-medium w-[40%]">Name</th>
              <th className="px-3 py-2 font-medium w-[15%]">Version</th>
              <th className="px-3 py-2 font-medium w-[15%]">Severity</th>
              <th className="px-3 py-2 font-medium w-[30%]">Details</th>
            </tr>
          </thead>
          <tbody>
            {groups.map((group) => {
              const isExpanded = expanded.has(group.key)
              const pkgNames = group.packages.map((p) => p.name)
              const displayNames = pkgNames.slice(0, 4)
              const truncated = pkgNames.length > 4
              const cveCount = group.packages.reduce((sum, p) => sum + (p.cveUrls?.length ?? 0), 0)
              const bugCount = group.packages.reduce((sum, p) => sum + (p.bugUrls?.length ?? 0), 0)
              const severityCount = group.severity === "security" ? cveCount || 1 : group.severity === "bugfix" ? bugCount || group.packages.length : 0
              const isGroupSelected = group.packages.every((p) => selectedSet.has(p.name))
              const isSecurity = group.severity === "security"

              // special package detection like kpatch handled via badge maybe not needed

              return (
                <React.Fragment key={group.key}>
                  <tr
                    className={cn(
                      "border-t hover:bg-muted/40",
                      isSecurity && "bg-destructive/5 hover:bg-destructive/10",
                      isExpanded && "bg-muted/30"
                    )}
                  >
                    <td className="px-2 py-2">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={isExpanded ? "Collapse" : "Expand"}
                        onClick={() => toggleExpanded(group.key)}
                      >
                        {isExpanded ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
                      </Button>
                    </td>
                    {selectable && (
                      <td className="px-2 py-2">
                        <Checkbox
                          aria-label={`Select ${pkgNames.join(", ")}`}
                          checked={isGroupSelected}
                          onCheckedChange={() => handleGroupToggle(group)}
                        />
                      </td>
                    )}
                    <td className="px-3 py-2">
                      <div className="flex flex-wrap items-center gap-1">
                        {displayNames.map((name, index) => {
                          const pkg = group.packages.find((p) => p.name === name)
                          const arch = pkg?.architecture ? ` (${pkg.architecture})` : ""
                          const tip = pkg ? `${pkg.name}${arch} ${pkg.summary ?? ""}` : name
                          return (
                            <Tooltip key={name}>
                              <TooltipTrigger>
                                <span className="font-medium">
                                  {name}
                                  {arch}
                                  {index !== displayNames.length - 1 || truncated ? ", " : ""}
                                </span>
                              </TooltipTrigger>
                              <TooltipContent>{tip}</TooltipContent>
                            </Tooltip>
                          )
                        })}
                        {truncated && <span className="text-muted-foreground">…</span>}
                        {group.packages.some((p) => p.name.startsWith("kpatch")) && (
                          <Badge variant="secondary" className="ml-1">
                            patches
                          </Badge>
                        )}
                      </div>
                    </td>
                    <td className="px-3 py-2 font-mono text-xs">
                      <span className="truncate block max-w-[12rem]" title={group.version}>
                        {group.version}
                      </span>
                    </td>
                    <td className="px-3 py-2">
                      <Tooltip>
                        <TooltipTrigger>
                          <span className="inline-flex items-center gap-1.5 tabular-nums">
                            {getSeverityIcon(group.severity)}
                            <span className="text-xs">{group.severity}</span>
                            {severityCount > 0 && group.severity !== "enhancement" ? (
                              <span className="text-xs font-medium">{severityCount}</span>
                            ) : null}
                          </span>
                        </TooltipTrigger>
                        <TooltipContent>{getSeverityLabel(group.severity)}</TooltipContent>
                      </Tooltip>
                    </td>
                    <td className="px-3 py-2 max-w-[24rem] truncate text-muted-foreground" title={firstLine(group.description)}>
                      {firstLine(group.description)}
                    </td>
                  </tr>
                  {isExpanded && (
                    <tr className="bg-muted/10">
                      <td colSpan={selectable ? 6 : 5} className="p-0">
                        <div className="grid gap-4 p-4 md:grid-cols-[280px_1fr]">
                          <dl className="space-y-3 text-sm">
                            <div>
                              <dt className="font-medium text-foreground">Packages</dt>
                              <dd className="text-muted-foreground break-words">
                                {group.packages.map((p) => p.name + (p.architecture ? ` (${p.architecture})` : "")).join(", ")}
                              </dd>
                            </div>
                            {group.packages.some((p) => p.cveUrls?.length) && (
                              <div>
                                <dt className="font-medium text-foreground">CVE</dt>
                                <dd className="flex flex-wrap gap-1">
                                  {Array.from(new Set(group.packages.flatMap((p) => p.cveUrls ?? []))).map((url) => {
                                    const label = url.match(/[^/=]+$/)?.[0] ?? url
                                    return (
                                      <a
                                        key={url}
                                        href={url}
                                        target="_blank"
                                        rel="noopener noreferrer"
                                        className="text-primary underline underline-offset-2 text-xs"
                                      >
                                        {label}
                                      </a>
                                    )
                                  })}
                                </dd>
                              </div>
                            )}
                            {group.packages[0]?.advisoryId && group.packages[0].advisoryId.startsWith("CVE") === false && (
                              <div>
                                <dt className="font-medium text-foreground">Severity</dt>
                                <dd className="text-xs">
                                  <Badge variant="outline" className="capitalize">
                                    {group.severity}
                                  </Badge>
                                </dd>
                              </div>
                            )}
                            {Array.from(new Set(group.packages.flatMap((p) => p.vendorUrls ?? []))).length > 0 && (
                              <div>
                                <dt className="font-medium text-foreground">Errata</dt>
                                <dd className="flex flex-wrap gap-1">
                                  {Array.from(new Set(group.packages.flatMap((p) => p.vendorUrls ?? []))).map((url) => {
                                    const label = url.match(/[^/=]+$/)?.[0] ?? url
                                    return (
                                      <a
                                        key={url}
                                        href={url}
                                        target="_blank"
                                        rel="noopener noreferrer"
                                        className="text-primary underline underline-offset-2 text-xs"
                                      >
                                        {label}
                                      </a>
                                    )
                                  })}
                                </dd>
                              </div>
                            )}
                            {group.packages.some((p) => p.bugUrls?.length) && (
                              <div>
                                <dt className="font-medium text-foreground">Bugs</dt>
                                <dd className="flex flex-wrap gap-1">
                                  {Array.from(new Set(group.packages.flatMap((p) => p.bugUrls ?? []))).map((url) => {
                                    const label = url.match(/[0-9]+$/)?.[0] ?? url
                                    return (
                                      <a
                                        key={url}
                                        href={url}
                                        target="_blank"
                                        rel="noopener noreferrer"
                                        className="text-primary underline underline-offset-2 text-xs"
                                      >
                                        {label}
                                      </a>
                                    )
                                  })}
                                </dd>
                              </div>
                            )}
                          </dl>
                          <div className="text-sm leading-relaxed">
                            <p className="whitespace-pre-wrap break-words text-muted-foreground">{group.description || "No additional details."}</p>
                            <div className="mt-3 grid grid-cols-2 gap-2 text-xs">
                              {group.packages.map((p) => (
                                <div key={p.name} className="rounded border bg-card p-2">
                                  <div className="font-medium">{p.name}</div>
                                  <div className="text-muted-foreground">
                                    {p.currentVersion ? `${p.currentVersion} → ${p.candidateVersion}` : p.candidateVersion}
                                  </div>
                                  {p.dependencies && p.dependencies.length > 0 && (
                                    <div className="mt-1 text-[11px]">Coupled with: {p.dependencies.join(", ")}</div>
                                  )}
                                </div>
                              ))}
                            </div>
                          </div>
                        </div>
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
