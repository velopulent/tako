import {
  createFileRoute,
  getRouteApi,
  useNavigate,
} from "@tanstack/react-router"

import { UserInventory } from "@/components/user-inventory"
import { qSearch } from "@/lib/search"

function UsersTab() {
  const search = getRouteApi("/accounts/users").useSearch()
  const navigate = useNavigate({ from: "/accounts/users" })
  const setQ = (q: string) =>
    navigate({
      search: (previous) => ({ ...previous, q: q || undefined }),
      replace: true,
    })
  return <UserInventory search={search.q} onSearchChange={setQ} />
}

export const Route = createFileRoute("/accounts/users")({
  validateSearch: qSearch,
  component: UsersTab,
})
