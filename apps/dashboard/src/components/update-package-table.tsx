import {
  Bug,
  ChevronDown,
  ChevronRight,
  ShieldAlert,
  Sparkles,
} from "lucide-react"
import * as React from "react"

import { AdvisoryMarkdown } from "@/components/advisory-markdown"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import type { UpdatePackage } from "@/lib/api"
import { cn } from "@/lib/utils"

function effectiveSeverity(severity?: string, secSeverity?: string) {
  const normalized = (severity ?? "").toLowerCase()
  if (normalized === "security" || secSeverity) return "security"
  if (normalized === "bugfix") return "bugfix"
  return "enhancement"
}

function severityVariant(severity: string) {
  if (severity === "security") return "destructive" as const
  if (severity === "bugfix") return "secondary" as const
  return "outline" as const
}

function severityLabel(severity?: string) {
  const normalized = (severity ?? "").toLowerCase()
  if (normalized === "security") return "Security"
  if (normalized === "bugfix") return "Bug fix"
  return "Enhancement"
}

function SeverityBadge({
  severity,
  count,
  secSeverity,
}: {
  severity?: string
  count?: number
  secSeverity?: string
}) {
  const normalized = effectiveSeverity(severity, secSeverity)
  const Icon =
    normalized === "security"
      ? ShieldAlert
      : normalized === "bugfix"
        ? Bug
        : Sparkles
  const label = severityLabel(severity)

  return (
    <Badge variant={severityVariant(normalized)}>
      <Icon data-icon="inline-start" aria-hidden="true" />
      {label}
      {count && count > 0 ? ` · ${count}` : ""}
      {secSeverity ? ` · ${secSeverity}` : ""}
    </Badge>
  )
}

function firstLine(text?: string) {
  if (!text) return "No additional details."
  const line = text.trim().split("\n")[0] ?? ""
  const clean = line.replace(/^[-*=\s]+/, "").trim()
  return clean.slice(0, 140) || "No additional details."
}

function packageLabel(pkg: UpdatePackage) {
  return pkg.name + (pkg.architecture ? ` (${pkg.architecture})` : "")
}

function versionSummary(group: Pick<AdvisoryGroup, "packages" | "version">) {
  const currentVersions = Array.from(
    new Set(
      group.packages
        .map((pkg) => pkg.currentVersion)
        .filter((version): version is string => Boolean(version))
    )
  )
  if (currentVersions.length === 0) return group.version
  return `${currentVersions.join(", ")} → ${group.version}`
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
    const key =
      pkg.groupKey?.trim() ||
      pkg.advisoryId?.trim() ||
      (pkg.candidateVersion ?? "") +
        "::" +
        (pkg.summary ?? "") +
        "::" +
        (pkg.severity ?? "")
    const list = map.get(key)
    if (list) list.push(pkg)
    else map.set(key, [pkg])
  }

  const severityRank = (pkg: UpdatePackage) => {
    const normalized = effectiveSeverity(pkg.severity, pkg.secSeverity)
    return normalized === "security" ? 3 : normalized === "bugfix" ? 2 : 1
  }

  const groups: AdvisoryGroup[] = []
  for (const [key, pkgs] of map.entries()) {
    pkgs.sort((a, b) => a.name.localeCompare(b.name))
    const highest = [...pkgs].sort(
      (a, b) => severityRank(b) - severityRank(a)
    )[0]
    groups.push({
      key,
      packages: pkgs,
      version: pkgs[0]?.candidateVersion ?? "",
      severity: effectiveSeverity(highest?.severity, highest?.secSeverity),
      description:
        highest?.description ?? highest?.details ?? highest?.summary ?? "",
    })
  }

  groups.sort((a, b) => {
    const rank = (severity: string) =>
      severity === "security" ? 0 : severity === "bugfix" ? 1 : 2
    const difference = rank(a.severity) - rank(b.severity)
    if (difference !== 0) return difference
    return a.packages[0].name.localeCompare(b.packages[0].name)
  })
  return groups
}

function uniqueUrls(
  packages: UpdatePackage[],
  key: "cveUrls" | "vendorUrls" | "bugUrls"
) {
  return Array.from(new Set(packages.flatMap((pkg) => pkg[key] ?? [])))
}

function ResourceLinks({
  label,
  urls,
  kind,
}: {
  label: string
  urls: string[]
  kind: "cve" | "errata" | "bug"
}) {
  if (urls.length === 0) return null

  return (
    <div className="flex flex-col gap-1.5">
      <dt className="font-medium text-foreground">{label}</dt>
      <dd className="flex flex-wrap gap-x-3 gap-y-1">
        {urls.map((url) => {
          const linkLabel =
            kind === "bug"
              ? (url.match(/[0-9]+$/)?.[0] ?? url)
              : (url.match(/[^/=]+$/)?.[0] ?? url)
          return (
            <a
              key={`${kind}:${url}`}
              href={url}
              target="_blank"
              rel="noopener noreferrer"
              className="break-all text-xs text-primary underline underline-offset-2"
            >
              {linkLabel}
            </a>
          )
        })}
      </dd>
    </div>
  )
}

function AdvisoryDetails({ group }: { group: AdvisoryGroup }) {
  const cveUrls = uniqueUrls(group.packages, "cveUrls")
  const errataUrls = uniqueUrls(group.packages, "vendorUrls")
  const bugUrls = uniqueUrls(group.packages, "bugUrls")

  return (
    <div className="grid gap-5 border-t bg-muted/10 p-4 lg:grid-cols-[minmax(14rem,0.7fr)_minmax(0,1.3fr)]">
      <dl className="flex flex-col gap-4 text-sm">
        <div className="flex flex-col gap-1.5">
          <dt className="font-medium text-foreground">Packages</dt>
          <dd className="break-words text-muted-foreground">
            {group.packages.map(packageLabel).join(", ")}
          </dd>
        </div>
        <div className="flex flex-col gap-1.5">
          <dt className="font-medium text-foreground">Severity</dt>
          <dd>
            <SeverityBadge
              severity={group.severity}
              secSeverity={
                group.packages.find((pkg) => pkg.secSeverity)?.secSeverity
              }
            />
          </dd>
        </div>
        <ResourceLinks label="CVE" urls={cveUrls} kind="cve" />
        <ResourceLinks label="Errata" urls={errataUrls} kind="errata" />
        <ResourceLinks label="Bugs" urls={bugUrls} kind="bug" />
      </dl>

      <div className="flex min-w-0 flex-col gap-4 text-sm leading-relaxed">
        {group.packages.some((pkg) => pkg.markdown) && group.description ? (
          <AdvisoryMarkdown text={group.description} />
        ) : (
          <p className="whitespace-pre-wrap break-words text-muted-foreground">
            {group.description || "No additional details."}
          </p>
        )}

        <div className="grid gap-2 sm:grid-cols-2">
          {group.packages.map((pkg) => (
            <div
              key={packageLabel(pkg)}
              className="min-w-0 rounded-lg border bg-card p-3"
            >
              <div className="break-words font-medium">{packageLabel(pkg)}</div>
              <div className="break-words font-mono text-xs text-muted-foreground">
                {pkg.currentVersion
                  ? `${pkg.currentVersion} → ${pkg.candidateVersion}`
                  : pkg.candidateVersion}
              </div>
              {pkg.dependencies && pkg.dependencies.length > 0 && (
                <div className="mt-2 break-words text-xs text-muted-foreground">
                  Dependencies: {pkg.dependencies.join(", ")}
                </div>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

function GroupSummary({
  group,
  expanded,
}: {
  group: AdvisoryGroup
  expanded: boolean
}) {
  const labels = group.packages.map(packageLabel)
  const visibleLabels = labels.slice(0, 3).join(", ")
  const remaining = labels.length - Math.min(labels.length, 3)
  const cveCount = uniqueUrls(group.packages, "cveUrls").length
  const bugCount = uniqueUrls(group.packages, "bugUrls").length
  const severityCount =
    group.severity === "security"
      ? cveCount || 1
      : group.severity === "bugfix"
        ? bugCount || group.packages.length
        : 0
  const hasPatch = group.packages.some((pkg) => pkg.name.startsWith("kpatch"))
  const secSeverity = group.packages.find((pkg) => pkg.secSeverity)?.secSeverity

  return (
    <div className="flex min-w-0 flex-1 items-start gap-3">
      <span
        className="mt-0.5 shrink-0 text-muted-foreground"
        aria-hidden="true"
      >
        {expanded ? <ChevronDown /> : <ChevronRight />}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="min-w-0 break-words font-medium">
            {visibleLabels}
          </span>
          {remaining > 0 && <Badge variant="outline">+{remaining} more</Badge>}
          {hasPatch && <Badge variant="secondary">Live patch</Badge>}
        </div>
        <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span className="font-mono break-all">{versionSummary(group)}</span>
          <span>
            {group.packages.length} package
            {group.packages.length === 1 ? "" : "s"}
          </span>
          <span className="min-w-0 max-w-full truncate">
            {firstLine(group.description)}
          </span>
        </div>
      </div>
      <SeverityBadge
        severity={group.severity}
        count={severityCount}
        secSeverity={secSeverity}
      />
    </div>
  )
}

export function UpdatePackageTable({
  packages,
}: {
  packages: UpdatePackage[]
}) {
  const groups = React.useMemo(() => buildGroups(packages), [packages])
  const [expanded, setExpanded] = React.useState<Set<string>>(() => new Set())

  const toggleExpanded = (key: string, open?: boolean) =>
    setExpanded((prev) => {
      const next = new Set(prev)
      const shouldOpen = open ?? !next.has(key)
      if (shouldOpen) next.add(key)
      else next.delete(key)
      return next
    })

  if (groups.length === 0) return null

  return (
    <div className="flex flex-col gap-2">
      <div className="hidden overflow-hidden rounded-lg border md:block">
        <Table className="table-fixed">
          <TableHeader className="bg-muted/50">
            <TableRow>
              <TableHead className="w-10">
                <span className="sr-only">Expand advisory</span>
              </TableHead>
              <TableHead className="w-[38%]">Package advisory</TableHead>
              <TableHead className="w-[18%]">Target version</TableHead>
              <TableHead className="w-[20%]">Severity</TableHead>
              <TableHead className="w-[24%]">Summary</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {groups.map((group) => {
              const isExpanded = expanded.has(group.key)
              return (
                <React.Fragment key={group.key}>
                  <TableRow
                    className={cn(
                      group.severity === "security" &&
                        "bg-destructive/5 hover:bg-destructive/10",
                      isExpanded && "bg-muted/30"
                    )}
                  >
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={
                          (isExpanded ? "Collapse" : "Expand") +
                          " advisory details for " +
                          group.packages[0].name
                        }
                        aria-expanded={isExpanded}
                        onClick={() => toggleExpanded(group.key)}
                      >
                        {isExpanded ? <ChevronDown /> : <ChevronRight />}
                      </Button>
                    </TableCell>
                    <TableCell className="whitespace-normal">
                      <div className="flex min-w-0 flex-col gap-1">
                        <span className="break-words font-medium">
                          {group.packages.map(packageLabel).join(", ")}
                        </span>
                        {group.packages.some((pkg) => pkg.advisoryId) && (
                          <span className="text-xs text-muted-foreground">
                            {
                              group.packages.find((pkg) => pkg.advisoryId)
                                ?.advisoryId
                            }
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="whitespace-normal">
                      <span className="break-all font-mono text-xs">
                        {versionSummary(group)}
                      </span>
                    </TableCell>
                    <TableCell>
                      <SeverityBadge
                        severity={group.severity}
                        count={
                          group.severity === "security"
                            ? uniqueUrls(group.packages, "cveUrls").length || 1
                            : group.severity === "bugfix"
                              ? uniqueUrls(group.packages, "bugUrls").length ||
                                group.packages.length
                              : undefined
                        }
                        secSeverity={
                          group.packages.find((pkg) => pkg.secSeverity)
                            ?.secSeverity
                        }
                      />
                    </TableCell>
                    <TableCell className="whitespace-normal text-muted-foreground">
                      <span className="line-clamp-2">
                        {firstLine(group.description)}
                      </span>
                    </TableCell>
                  </TableRow>
                  {isExpanded && (
                    <TableRow>
                      <TableCell colSpan={5} className="p-0 whitespace-normal">
                        <AdvisoryDetails group={group} />
                      </TableCell>
                    </TableRow>
                  )}
                </React.Fragment>
              )
            })}
          </TableBody>
        </Table>
      </div>

      <div className="flex flex-col gap-2 md:hidden">
        {groups.map((group) => {
          const isExpanded = expanded.has(group.key)
          return (
            <Card
              key={group.key}
              size="sm"
              className={cn(
                "overflow-hidden",
                group.severity === "security" && "border-destructive/30"
              )}
            >
              <Collapsible
                open={isExpanded}
                onOpenChange={(open) => toggleExpanded(group.key, open)}
              >
                <CollapsibleTrigger
                  render={
                    <Button
                      variant="ghost"
                      className="h-auto w-full justify-start rounded-none p-3 text-left"
                      aria-label={
                        (isExpanded ? "Collapse" : "Expand") +
                        " advisory details for " +
                        group.packages[0].name
                      }
                      aria-expanded={isExpanded}
                    />
                  }
                >
                  <GroupSummary group={group} expanded={isExpanded} />
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <AdvisoryDetails group={group} />
                </CollapsibleContent>
              </Collapsible>
            </Card>
          )
        })}
      </div>
    </div>
  )
}
