import {
  createFileRoute,
  Outlet,
  useLocation,
  useNavigate,
} from "@tanstack/react-router"
import { ChevronDownIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Page } from "@/lib/page"

const tabs = [
  { value: "users", label: "Users", to: "/accounts/users" },
  { value: "groups", label: "Groups", to: "/accounts/groups" },
  { value: "passwords", label: "Passwords", to: "/accounts/passwords" },
  { value: "ssh-keys", label: "SSH Keys", to: "/accounts/ssh-keys" },
] as const

function AccountsLayout() {
  const location = useLocation()
  const navigate = useNavigate()
  const active = (() => {
    if (location.pathname.startsWith("/accounts/groups")) return "groups"
    if (location.pathname.startsWith("/accounts/passwords")) return "passwords"
    if (location.pathname.startsWith("/accounts/ssh-keys")) return "ssh-keys"
    return "users"
  })()

  return (
    <Page description="Local accounts and groups. Local entries are mutable; remote NSS identities remain read-only.">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Tabs
          value={active}
          onValueChange={(value) => {
            const tab = tabs.find((t) => t.value === value)
            if (tab) navigate({ to: tab.to })
          }}
        >
          <TabsList>
            {tabs.map((tab) => (
              <TabsTrigger key={tab.value} value={tab.value}>
                {tab.label}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <DropdownMenu>
          <DropdownMenuTrigger render={<Button variant="outline" />}>
            New <ChevronDownIcon data-icon="inline-end" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              onClick={() => {
                if (active !== "users") navigate({ to: "/accounts/users" })
                window.dispatchEvent(new CustomEvent("accounts:create-user"))
              }}
            >
              Create user
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => {
                if (active !== "groups") navigate({ to: "/accounts/groups" })
                window.dispatchEvent(new CustomEvent("accounts:create-group"))
              }}
            >
              Create group
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <Outlet />
    </Page>
  )
}

export const Route = createFileRoute("/accounts")({
  component: AccountsLayout,
})
