import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query"
import { RouterProvider, createRouter } from "@tanstack/react-router"

import { LoginPage } from "@/components/login-page"
import { TooltipProvider } from "@/components/ui/tooltip"
import { api, type SessionResponse } from "@/lib/api"
import { routeTree } from "./routeTree.gen"

const router = createRouter({
  routeTree,
  context: { session: undefined! },
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

  if (session.isPending) {
    return <div className="min-h-svh bg-background" />
  }

  if (!session.data) {
    return (
      <LoginPage
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
