import {
  createFileRoute,
  Link,
  Outlet,
  useLocation,
} from "@tanstack/react-router"

import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Page } from "@/lib/page"

function AccountsLayout() {
  const groups = useLocation({
    select: (location) => location.pathname.startsWith("/accounts/groups"),
  })
  return (
    <Page description="People and groups on this host.">
      <Tabs value={groups ? "groups" : "users"}>
        <TabsList>
          <TabsTrigger value="users" render={<Link to="/accounts/users" />}>
            Users
          </TabsTrigger>
          <TabsTrigger value="groups" render={<Link to="/accounts/groups" />}>
            Groups
          </TabsTrigger>
        </TabsList>
      </Tabs>
      <Outlet />
    </Page>
  )
}

export const Route = createFileRoute("/accounts")({ component: AccountsLayout })
