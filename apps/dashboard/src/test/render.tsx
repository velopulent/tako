import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render } from "@testing-library/react"
import type * as React from "react"
import { vi } from "vitest"

import { AdminAccessContext } from "@/components/admin-access"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { SessionResponse } from "@/lib/api"
import { sessionKey } from "@/lib/session"

export const testSession: SessionResponse = {
  user: { username: "octopus", name: "Octopus", uid: 1000, gid: 1000 },
  csrfToken: "csrf-token",
}

// renderWithProviders renders UI inside the providers the authenticated app
// supplies: a query client seeded with a session, tooltips, and a fixed
// administrative-access state.
export function renderWithProviders(
  ui: React.ReactElement,
  { administrative = false }: { administrative?: boolean } = {}
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.setQueryDefaults(sessionKey, {
    staleTime: Number.POSITIVE_INFINITY,
    refetchInterval: false,
  })
  client.setQueryData(sessionKey, testSession)
  const access = {
    administrative,
    request: vi.fn(),
    drop: vi.fn(async () => {}),
  }
  const result = render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <AdminAccessContext.Provider value={access}>
          {ui}
        </AdminAccessContext.Provider>
      </TooltipProvider>
    </QueryClientProvider>
  )
  return { ...result, client, access }
}

export function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}
