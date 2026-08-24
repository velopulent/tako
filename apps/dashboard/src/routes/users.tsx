import { createFileRoute, getRouteApi, useNavigate } from "@tanstack/react-router"

import { GroupInventory } from "@/components/group-inventory"
import { PasswordManager } from "@/components/password-manager"
import { SSHKeyManager } from "@/components/ssh-key-manager"
import { UserInventory } from "@/components/user-inventory"
import { qSearch } from "@/lib/search"
import { Page } from "@/lib/page"

function UsersPage() {
  const search = getRouteApi("/users").useSearch()
  const navigate = useNavigate({ from: "/users" })
  const setQ = (q: string) =>
    navigate({
      search: (previous) => ({ ...previous, q: q || undefined }),
      replace: true,
    })
  return (
    <Page description="NSS account inventory. Local entries are explicitly mutable; remote identities remain read-only.">
      <UserInventory search={search.q} onSearchChange={setQ} />
      <GroupInventory search={search.q} onSearchChange={setQ} />
      <PasswordManager />
      <SSHKeyManager />
    </Page>
  )
}

export const Route = createFileRoute("/users")({
  validateSearch: qSearch,
  component: UsersPage,
})
