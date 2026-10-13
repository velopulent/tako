import { api } from "@/lib/api"

type Preview = {
  current: { fingerprint: string }
  allowed: boolean
  stale: boolean
  reason?: string
  warnings?: string[]
}

// guardedChange previews an administrative change, refuses what the host
// policy rejects, then applies exactly the previewed state. Every account
// endpoint pairs `<path>/preview` with `<path>`.
export async function guardedChange<T = unknown>(
  path: string,
  operation: Record<string, unknown>,
  apply: (preview: Preview) => Record<string, unknown> = (preview) => ({
    expectedFingerprint: preview.current.fingerprint,
  })
) {
  const preview = await api<Preview>(`${path}/preview`, {
    method: "POST",
    body: JSON.stringify(operation),
  })
  if (!preview.allowed)
    throw new Error(
      preview.reason || preview.warnings?.[0] || "The host refused this change."
    )
  return api<T>(path, {
    method: "POST",
    body: JSON.stringify({ ...operation, ...apply(preview) }),
  })
}
