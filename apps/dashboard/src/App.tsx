import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query"
import { createRouter, RouterProvider } from "@tanstack/react-router"
import { useEffect } from "react"

import { LoginPage } from "@/components/login-page"
import { TooltipProvider } from "@/components/ui/tooltip"
import { api, type BrandingResponse, type SessionResponse } from "@/lib/api"
import { routeTree } from "./routeTree.gen"

const router = createRouter({
  routeTree,
  context: { session: undefined as unknown as SessionResponse },
})

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router
  }
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, refetchOnWindowFocus: false },
  },
})

function AuthenticatedApp() {
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const branding = useQuery({
    queryKey: ["branding"],
    queryFn: () => api<BrandingResponse>("/branding"),
    retry: 1,
    staleTime: 5 * 60 * 1000,
  })
  const hostname = branding.data?.hostname?.trim()

  useEffect(() => {
    document.title = hostname ? `${hostname} | Tako` : "Tako"
  }, [hostname])

  if (session.isPending) {
    return (
      <LoginPage
        branding={branding.data}
        onAuthenticated={(value) =>
          queryClient.setQueryData(["session"], value)
        }
      />
    )
  }

  if (!session.data) {
    return (
      <LoginPage
        branding={branding.data}
        onAuthenticated={(value) =>
          queryClient.setQueryData(["session"], value)
        }
      />
    )
  }

  return <RouterProvider router={router} context={{ session: session.data }} />
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <AuthenticatedApp />
      </TooltipProvider>
    </QueryClientProvider>
  )
}
