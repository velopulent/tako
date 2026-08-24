import { createFileRoute, getRouteApi, useNavigate } from "@tanstack/react-router"

import { GroupInventory } from "@/components/group-inventory"
import { qSearch } from "@/lib/search"

function GroupsTab() {
  const search = getRouteApi("/accounts/groups").useSearch()
  const navigate = useNavigate({ from: "/accounts/groups" })
  const setQ = (q: string) =>
    navigate({
      search: (previous) => ({ ...previous, q: q || undefined }),
      replace: true,
    })
  return <GroupInventory search={search.q} onSearchChange={setQ} />
}

export const Route = createFileRoute("/accounts/groups")({
  validateSearch: qSearch,
  component: GroupsTab,
})
