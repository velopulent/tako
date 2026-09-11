export function parseString(value: unknown): string {
  return typeof value === "string" ? value : ""
}

export function parseEnum<T extends string>(
  value: unknown,
  allowed: readonly T[],
  fallback: T
): T {
  return allowed.includes(value as T) ? (value as T) : fallback
}

export const metricRanges = ["15m", "1h", "6h", "24h"] as const
export type MetricRange = (typeof metricRanges)[number]

export const serviceTypes = [
  "service",
  "target",
  "socket",
  "timer",
  "path",
] as const
export const serviceScopes = ["system", "user"] as const
export const serviceActiveStates = [
  "all",
  "active",
  "inactive",
  "failed",
] as const
export const serviceFileStates = [
  "all",
  "enabled",
  "disabled",
  "static",
  "masked",
] as const

function optionalString(value: unknown): string | undefined {
  const parsed = parseString(value)
  return parsed || undefined
}

export function qSearch(search: Record<string, unknown>): { q?: string } {
  return { q: optionalString(search.q) }
}

export const networkViews = [
  "overview",
  "configuration",
  "firewall",
  "logs",
] as const
export type NetworkView = (typeof networkViews)[number]

export function networkSearch(search: Record<string, unknown>): {
  q?: string
  view?: NetworkView
} {
  const view = parseEnum(search.view, networkViews, "overview")
  return {
    q: optionalString(search.q),
    view: view === "overview" ? undefined : view,
  }
}

export function processDetailSearch(search: Record<string, unknown>): {
  q?: string
  started?: string
} {
  return {
    q: optionalString(search.q),
    started: optionalString(search.started),
  }
}

export function metricsSearch(search: Record<string, unknown>): {
  q?: string
  range?: MetricRange
} {
  const range = parseEnum(search.range, metricRanges, "1h")
  return {
    q: optionalString(search.q),
    range: range === "1h" ? undefined : range,
  }
}

export function servicesSearch(search: Record<string, unknown>): {
  q?: string
  type?: (typeof serviceTypes)[number]
  scope?: (typeof serviceScopes)[number]
  active?: (typeof serviceActiveStates)[number]
  file?: (typeof serviceFileStates)[number]
} {
  const type = parseEnum(search.type, serviceTypes, "service")
  const scope = parseEnum(search.scope, serviceScopes, "system")
  const active = parseEnum(search.active, serviceActiveStates, "all")
  const file = parseEnum(search.file, serviceFileStates, "all")
  return {
    q: optionalString(search.q),
    type: type === "service" ? undefined : type,
    scope: scope === "system" ? undefined : scope,
    active: active === "all" ? undefined : active,
    file: file === "all" ? undefined : file,
  }
}

export function logsSearch(search: Record<string, unknown>): {
  q?: string
  priority?: string
  boot?: string
  since?: string
  until?: string
  unit?: string
  executable?: string
  text?: string
} {
  return {
    q: optionalString(search.q),
    priority: optionalString(search.priority),
    boot: optionalString(search.boot),
    since: optionalString(search.since),
    until: optionalString(search.until),
    unit: optionalString(search.unit),
    executable: optionalString(search.executable),
    text: optionalString(search.text),
  }
}
