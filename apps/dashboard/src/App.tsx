import {
  MutationCache,
  QueryCache,
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query"
import { createRouter, RouterProvider } from "@tanstack/react-router"
import { useEffect } from "react"

import { AdminAccessProvider } from "@/components/admin-access"
import { LoginPage } from "@/components/login-page"
import { Toaster } from "@/components/ui/toast"
import { TooltipProvider } from "@/components/ui/tooltip"
import { useActivityHeartbeat } from "@/hooks/use-activity-heartbeat"
import { APIError, api, type BrandingResponse, setCSRFToken } from "@/lib/api"
import { loadSession, sessionKey } from "@/lib/session"
import { routeTree } from "./routeTree.gen"

const router = createRouter({
  routeTree,
  defaultPreload: "intent",
  scrollRestoration: true,
})

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router
  }
}

// An expired or revoked session returns 401 from any endpoint. Dropping the
// cached session swaps the whole app back to the sign-in screen.
function signOutOnExpiry(error: unknown) {
  if (error instanceof APIError && error.status === 401) {
    setCSRFToken("")
    queryClient.setQueryData(sessionKey, null)
  }
}

const queryClient: QueryClient = new QueryClient({
  queryCache: new QueryCache({ onError: signOutOnExpiry }),
  mutationCache: new MutationCache({ onError: signOutOnExpiry }),
  defaultOptions: {
    queries: { retry: false, refetchOnWindowFocus: false },
  },
})

function AuthenticatedApp() {
  const session = useQuery({ queryKey: sessionKey, queryFn: loadSession })
  const branding = useQuery({
    queryKey: ["branding"],
    queryFn: () => api<BrandingResponse>("/branding"),
    retry: 1,
    staleTime: 5 * 60 * 1000,
  })
  const hostname = branding.data?.hostname?.trim()
  useActivityHeartbeat(Boolean(session.data))

  useEffect(() => {
    document.title = hostname ? `${hostname} · Tako` : "Tako"
  }, [hostname])

  if (session.isPending) {
    return (
      <div
        className="min-h-svh bg-background"
        data-testid="auth-pending-shell"
      />
    )
  }

  if (!session.data) {
    return (
      <LoginPage
        branding={branding.data}
        onAuthenticated={(value) => {
          setCSRFToken(value.csrfToken)
          // Drop data from any previous user, but keep the session query
          // itself: clearing it would detach this component's observer.
          queryClient.removeQueries({
            predicate: (query) => query.queryKey[0] !== sessionKey[0],
          })
          queryClient.setQueryData(sessionKey, value)
        }}
      />
    )
  }

  return (
    <AdminAccessProvider>
      <RouterProvider router={router} />
    </AdminAccessProvider>
  )
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <Toaster>
          <AuthenticatedApp />
        </Toaster>
      </TooltipProvider>
    </QueryClientProvider>
  )
}
