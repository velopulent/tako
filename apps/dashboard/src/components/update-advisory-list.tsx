import {
  Bug,
  ChevronDown,
  ExternalLink,
  Package,
  SearchIcon,
  ShieldAlert,
  Sparkles,
  X,
} from "lucide-react"
import * as React from "react"

import { AdvisoryMarkdown } from "@/components/advisory-markdown"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@/components/ui/input-group"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import type { UpdatePackage } from "@/lib/api"
import {
  type AdvisoryGroup,
  buildAdvisoryGroups,
  firstLine,
  formatAbsolute,
  formatRelative,
  groupMatches,
  plural,
  referenceLabel,
  type SecuritySeverity,
  type UpdateSeverity,
  versionDiff,
} from "@/lib/update-advisories"
import { cn } from "@/lib/utils"

const pageSize = 40

const severityMeta: Record<
  UpdateSeverity,
  { label: string; filter: string; icon: typeof ShieldAlert }
> = {
  security: { label: "Security", filter: "Security", icon: ShieldAlert },
  bugfix: { label: "Bug fix", filter: "Bug fixes", icon: Bug },
  enhancement: { label: "Enhancement", filter: "Enhancements", icon: Sparkles },
  other: { label: "Update", filter: "Other", icon: Package },
}

const securityLabel: Record<SecuritySeverity, string> = {
  critical: "Critical",
  important: "Important",
  moderate: "Moderate",
  low: "Low",
}

export function SeverityBadge({
  severity,
  secSeverity,
}: {
  severity: UpdateSeverity
  secSeverity?: SecuritySeverity
}) {
  if (severity === "other") return null
  const { label, icon: Icon } = severityMeta[severity]
  return (
    <Badge variant={severity === "security" ? "destructive" : "outline"}>
      <Icon data-icon="inline-start" aria-hidden="true" />
      {secSeverity ? securityLabel[secSeverity] : label}
      {secSeverity && <span className="sr-only"> security</span>}
    </Badge>
  )
}

export function VersionDiff({
  from,
  to,
  className,
}: {
  from?: string
  to: string
  className?: string
}) {
  const diff = versionDiff(from, to)
  return (
    <span
      className={cn("font-mono text-xs tabular-nums", className)}
      title={from ? `${from} → ${to}` : to}
    >
      {from && (
        <>
          <span className="sr-only">from </span>
          <span className="text-muted-foreground">
            {diff.common}
            {diff.from}
          </span>
          <span className="px-1 text-muted-foreground" aria-hidden="true">
            →
          </span>
          <span className="sr-only"> to </span>
        </>
      )}
      <span className="text-muted-foreground">{diff.common}</span>
      <span className="font-medium text-foreground">{diff.to}</span>
    </span>
  )
}

function groupVersion(group: AdvisoryGroup) {
  const targets = new Set(group.packages.map((pkg) => pkg.candidateVersion))
  if (targets.size !== 1) return null
  const currents = new Set(
    group.packages.map((pkg) => pkg.currentVersion ?? "")
  )
  const [to] = targets
  const [from] = currents.size === 1 ? currents : [undefined]
  return { from: from || undefined, to }
}

function SeverityMedia({ group }: { group: AdvisoryGroup }) {
  const Icon = severityMeta[group.severity].icon
  return (
    <span
      className={cn(
        "flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground",
        group.severity === "security" && "bg-destructive/10 text-destructive"
      )}
      aria-hidden="true"
    >
      <Icon className="size-4" />
    </span>
  )
}

function GroupRow({ group }: { group: AdvisoryGroup }) {
  const version = groupVersion(group)
  const summary = firstLine(group.description)
  const meta = [
    group.advisoryId,
    group.issued ? formatRelative(group.issued) : "",
    summary,
  ].filter(Boolean)

  return (
    <>
      <SeverityMedia group={group} />
      <span className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate font-medium" title={group.title}>
            {group.title}
          </span>
          {group.packages.length > 1 && (
            <Badge variant="secondary" className="tabular-nums">
              {plural(group.packages.length, "package")}
            </Badge>
          )}
        </span>
        {meta.length > 0 && (
          <span className="truncate text-xs text-muted-foreground">
            {meta.join(" · ")}
          </span>
        )}
        {version && <VersionDiff {...version} className="truncate sm:hidden" />}
      </span>
      {version && (
        <VersionDiff
          {...version}
          className="hidden max-w-64 shrink-0 truncate text-right sm:block"
        />
      )}
      <span className="hidden w-28 shrink-0 justify-end md:flex">
        <SeverityBadge
          severity={group.severity}
          secSeverity={group.secSeverity}
        />
      </span>
      <ChevronDown
        className="size-4 shrink-0 text-muted-foreground transition-transform duration-200 group-data-[panel-open]/row:rotate-180 motion-reduce:transition-none"
        aria-hidden="true"
      />
    </>
  )
}

function ReferenceLinks({ label, urls }: { label: string; urls: string[] }) {
  if (urls.length === 0) return null
  return (
    <div className="flex flex-col gap-1.5">
      <dt className="text-xs font-medium text-muted-foreground">{label}</dt>
      <dd className="flex flex-wrap gap-1.5">
        {urls.map((url) => (
          <Badge
            key={url}
            variant="outline"
            render={
              <a
                href={url}
                target="_blank"
                rel="noopener noreferrer nofollow"
              />
            }
          >
            {referenceLabel(url)}
            <ExternalLink data-icon="inline-end" aria-hidden="true" />
          </Badge>
        ))}
      </dd>
    </div>
  )
}

function GroupDetails({ group }: { group: AdvisoryGroup }) {
  return (
    <div className="flex flex-col gap-4 border-t bg-muted/30 px-4 py-4 sm:pl-15">
      <div className="flex flex-col gap-3 md:hidden">
        <SeverityBadge
          severity={group.severity}
          secSeverity={group.secSeverity}
        />
      </div>

      {group.description &&
        (group.markdown ? (
          <div className="max-w-prose">
            <AdvisoryMarkdown text={group.description} />
          </div>
        ) : (
          <p className="max-w-prose text-sm leading-relaxed break-words whitespace-pre-line text-muted-foreground">
            {group.description}
          </p>
        ))}

      {(group.advisoryId ||
        group.issued ||
        group.cveUrls.length > 0 ||
        group.bugUrls.length > 0 ||
        group.vendorUrls.length > 0) && (
        <dl className="flex flex-wrap gap-x-8 gap-y-3 text-sm">
          {group.advisoryId && (
            <div className="flex flex-col gap-1.5">
              <dt className="text-xs font-medium text-muted-foreground">
                Advisory
              </dt>
              <dd className="font-mono text-xs">{group.advisoryId}</dd>
            </div>
          )}
          {group.issued && (
            <div className="flex flex-col gap-1.5">
              <dt className="text-xs font-medium text-muted-foreground">
                Published
              </dt>
              <dd className="text-xs">
                <time dateTime={group.issued}>
                  {formatAbsolute(group.issued)}
                </time>
              </dd>
            </div>
          )}
          <ReferenceLinks label="CVEs" urls={group.cveUrls} />
          <ReferenceLinks label="Bug reports" urls={group.bugUrls} />
          <ReferenceLinks label="Vendor advisory" urls={group.vendorUrls} />
        </dl>
      )}

      <div className="flex flex-col gap-1.5">
        <h4 className="text-xs font-medium text-muted-foreground">
          {plural(group.packages.length, "package")}
        </h4>
        <ul className="flex max-h-80 flex-col overflow-y-auto overscroll-contain rounded-lg border bg-card">
          {group.packages.map((pkg) => (
            <PackageLine key={pkg.name + (pkg.architecture ?? "")} pkg={pkg} />
          ))}
        </ul>
      </div>
    </div>
  )
}

function PackageLine({ pkg }: { pkg: UpdatePackage }) {
  return (
    <li className="flex flex-col gap-0.5 px-3 py-2 not-last:border-b sm:flex-row sm:items-center sm:justify-between sm:gap-4">
      <span className="min-w-0 truncate text-sm">
        {pkg.name}
        {pkg.architecture && (
          <span className="text-muted-foreground">.{pkg.architecture}</span>
        )}
      </span>
      <VersionDiff
        from={pkg.currentVersion}
        to={pkg.candidateVersion}
        className="min-w-0 truncate sm:text-right"
      />
      {pkg.dependencies && pkg.dependencies.length > 0 && (
        <span className="text-xs text-muted-foreground sm:basis-full">
          Pulls in {pkg.dependencies.join(", ")}
        </span>
      )}
    </li>
  )
}

type Filter = "all" | UpdateSeverity

export function UpdateAdvisoryList({
  packages,
}: {
  packages: UpdatePackage[]
}) {
  const groups = React.useMemo(() => buildAdvisoryGroups(packages), [packages])
  const [search, setSearch] = React.useState("")
  const [filter, setFilter] = React.useState<Filter>("all")
  const [expanded, setExpanded] = React.useState<Set<string>>(() => new Set())
  const [limit, setLimit] = React.useState(pageSize)
  const query = React.useDeferredValue(search.trim())

  const counts = React.useMemo(() => {
    const value: Record<UpdateSeverity, number> = {
      security: 0,
      bugfix: 0,
      enhancement: 0,
      other: 0,
    }
    for (const group of groups) value[group.severity] += 1
    return value
  }, [groups])
  const classified = counts.other < groups.length
  const filters = (Object.keys(counts) as UpdateSeverity[]).filter(
    (severity) => counts[severity] > 0
  )

  const visible = groups.filter(
    (group) =>
      (filter === "all" || group.severity === filter) &&
      groupMatches(group, query)
  )
  const shown = visible.slice(0, limit)
  const filtered = filter !== "all" || query !== ""

  const toggle = (key: string, open: boolean) =>
    setExpanded((current) => {
      const next = new Set(current)
      if (open) next.add(key)
      else next.delete(key)
      return next
    })
  const clearFilters = () => {
    setSearch("")
    setFilter("all")
  }

  if (groups.length === 0) return null

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <InputGroup className="min-w-0 flex-1 basis-56">
          <InputGroupAddon>
            <SearchIcon aria-hidden="true" />
          </InputGroupAddon>
          <InputGroupInput
            type="search"
            placeholder="Search packages, advisories, CVEs"
            aria-label="Search updates"
            value={search}
            onChange={(event) => {
              setSearch(event.target.value)
              setLimit(pageSize)
            }}
            onKeyDown={(event) => {
              if (event.key === "Escape" && search) {
                event.stopPropagation()
                setSearch("")
              }
            }}
          />
          {search && (
            <InputGroupAddon align="inline-end">
              <InputGroupButton
                size="icon-xs"
                aria-label="Clear search"
                onClick={() => setSearch("")}
              >
                <X />
              </InputGroupButton>
            </InputGroupAddon>
          )}
        </InputGroup>

        {classified && filters.length > 1 && (
          <ToggleGroup
            aria-label="Filter by update type"
            variant="outline"
            size="sm"
            spacing={0}
            className="max-w-full shrink-0 overflow-x-auto p-px"
            value={[filter]}
            onValueChange={(values: string[]) => {
              setFilter((values[values.length - 1] as Filter) ?? "all")
              setLimit(pageSize)
            }}
          >
            <ToggleGroupItem value="all">
              All
              <span className="text-muted-foreground tabular-nums">
                {groups.length}
              </span>
            </ToggleGroupItem>
            {filters.map((severity) => (
              <ToggleGroupItem key={severity} value={severity}>
                {severityMeta[severity].filter}
                <span className="text-muted-foreground tabular-nums">
                  {counts[severity]}
                </span>
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        )}
      </div>

      <p className="sr-only" role="status" aria-live="polite">
        {filtered
          ? `${plural(visible.length, "update")} match the current filters.`
          : ""}
      </p>

      {visible.length === 0 ? (
        <Empty className="border py-10">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchIcon />
            </EmptyMedia>
            <EmptyTitle>No matching updates</EmptyTitle>
            <EmptyDescription>
              Nothing matches
              {query ? ` “${query}”` : " this filter"}. Try a package name,
              advisory ID, or CVE.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button variant="outline" size="sm" onClick={clearFilters}>
              Clear filters
            </Button>
          </EmptyContent>
        </Empty>
      ) : (
        <ul className="overflow-hidden rounded-xl border bg-card">
          {shown.map((group) => {
            const open = expanded.has(group.key)
            return (
              <li key={group.key} className="not-last:border-b">
                <Collapsible
                  open={open}
                  onOpenChange={(next) => toggle(group.key, next)}
                >
                  <CollapsibleTrigger
                    className={cn(
                      "group/row flex w-full items-center gap-3 px-4 py-3 text-left outline-none transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset",
                      group.severity === "security" &&
                        "bg-destructive/[0.03] hover:bg-destructive/[0.06]"
                    )}
                  >
                    <GroupRow group={group} />
                  </CollapsibleTrigger>
                  <CollapsibleContent>
                    <GroupDetails group={group} />
                  </CollapsibleContent>
                </Collapsible>
              </li>
            )
          })}
        </ul>
      )}

      {visible.length > shown.length && (
        <Button
          variant="outline"
          className="self-center"
          onClick={() => setLimit((current) => current + pageSize * 5)}
        >
          Show {Math.min(visible.length - shown.length, pageSize * 5)} more
          <span className="text-muted-foreground">
            of {visible.length - shown.length}
          </span>
        </Button>
      )}
    </div>
  )
}
