const byteUnits = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"]

// formatBytes renders a byte count in binary units with one decimal above 1 KiB.
export function formatBytes(value: number | undefined | null) {
  if (value === undefined || value === null || !Number.isFinite(value))
    return "—"
  let amount = Math.max(0, value)
  let unit = 0
  while (amount >= 1024 && unit < byteUnits.length - 1) {
    amount /= 1024
    unit++
  }
  const digits = unit === 0 || amount >= 100 ? 0 : 1
  return `${amount.toFixed(digits)} ${byteUnits[unit]}`
}

export function formatRate(value: number | undefined | null) {
  return value === undefined || value === null ? "—" : `${formatBytes(value)}/s`
}

export function formatPercent(value: number | undefined | null, digits = 1) {
  if (value === undefined || value === null || !Number.isFinite(value))
    return "—"
  return `${value.toFixed(digits)}%`
}

// formatUptime renders a duration in seconds as "3d 4h", "4h 12m", or "12m".
export function formatUptime(seconds: number) {
  const days = Math.floor(seconds / 86_400)
  const hours = Math.floor((seconds % 86_400) / 3_600)
  const minutes = Math.floor((seconds % 3_600) / 60)
  if (days) return `${days}d ${hours}h`
  if (hours) return `${hours}h ${minutes}m`
  return `${minutes}m`
}

const relative = new Intl.RelativeTimeFormat(undefined, {
  numeric: "auto",
  style: "long",
})
const relativeSteps: [Intl.RelativeTimeFormatUnit, number][] = [
  ["second", 60],
  ["minute", 60],
  ["hour", 24],
  ["day", 30],
  ["month", 12],
  ["year", Number.POSITIVE_INFINITY],
]

// formatRelative renders a timestamp relative to now, such as "5 minutes ago".
export function formatRelative(
  value: string | number | Date,
  now = Date.now()
) {
  const time = new Date(value).getTime()
  if (!Number.isFinite(time)) return "—"
  let amount = (time - now) / 1000
  for (const [unit, size] of relativeSteps) {
    if (Math.abs(amount) < size)
      return relative.format(Math.round(amount), unit)
    amount /= size
  }
  return relative.format(Math.round(amount), "year")
}

const dateTime = new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "medium",
})
const shortDateTime = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
})
const clock = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
})

export function formatDateTime(value: string | number | Date) {
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? dateTime.format(date) : "—"
}

// formatTimestamp is compact: "Oct 12, 07:56:27 PM".
export function formatTimestamp(value: string | number | Date) {
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? shortDateTime.format(date) : "—"
}

const clockSeconds = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
})

// clockFormatter picks a tick format that stays distinct over the span.
export function clockFormatter(first?: string, last?: string) {
  const span = first && last ? Date.parse(last) - Date.parse(first) : 0
  return (value: string | number | Date) => {
    const date = new Date(value)
    if (!Number.isFinite(date.getTime())) return ""
    return span < 10 * 60_000 ? clockSeconds.format(date) : clock.format(date)
  }
}

export function formatClock(value: string | number | Date) {
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? clock.format(date) : ""
}

export function plural(count: number, one: string, many = `${one}s`) {
  return `${count.toLocaleString()} ${count === 1 ? one : many}`
}
