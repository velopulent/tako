import * as React from "react"
import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query"
import {
  Outlet,
  RouterProvider,
  createRootRouteWithContext,
  createRoute,
  createRouter,
} from "@tanstack/react-router"

import { AppSidebar } from "@/components/app-sidebar"
import { LoginPage } from "@/components/login-page"
import { SiteHeader } from "@/components/site-header"
import { TooltipProvider } from "@/components/ui/tooltip"
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"
import { DashboardPage } from "@/routes/dashboard"
import { ModulePage } from "@/routes/module"
import { ServiceDetailPage } from "@/routes/service-detail"
import { TimersPage } from "@/routes/timers"
import { api, type SessionResponse } from "@/lib/api"

type RouterContext = {
  session: SessionResponse
}

function AuthenticatedLayout() {
  const { session } = rootRoute.useRouteContext()

  return (
    <SidebarProvider
      style={
        {
          "--sidebar-width": "calc(var(--spacing) * 66)",
          "--header-height": "calc(var(--spacing) * 14)",
        } as React.CSSProperties
      }
    >
      <AppSidebar user={session.user} variant="inset" />
      <SidebarInset>
        <SiteHeader user={session.user} csrfToken={session.csrfToken} />
        <Outlet />
      </SidebarInset>
    </SidebarProvider>
  )
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: AuthenticatedLayout,
})
const dashboardRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: DashboardPage,
})

const moduleNames = [
  "logs",
  "users",
  "updates",
  "operations",
  "jobs",
  "host",
  "terminal",
  "metrics",
  "services",
  "storage",
  "network",
  "files",
  "processes",
  "settings",
] as const

const moduleRoutes = moduleNames.map((module) =>
  createRoute({
    getParentRoute: () => rootRoute,
    path: module,
    component: () => <ModulePage module={module} />,
  })
)

const serviceDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/services/$scope/$unit",
  component: ServiceDetailPage,
})

const timersRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/timers",
  component: TimersPage,
})

const routeTree = rootRoute.addChildren([
  dashboardRoute,
  ...moduleRoutes,
  serviceDetailRoute,
  timersRoute,
])
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
