import { useLocation } from "@tanstack/react-router"
import { LogOutIcon, MoonIcon, SunIcon } from "lucide-react"

import type { User } from "@/lib/api"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { SidebarTrigger } from "@/components/ui/sidebar"
import { useTheme } from "@/components/theme-provider"

const titles: Record<string, string> = {
  "/": "Dashboard",
  "/logs": "System logs",
  "/users": "Users",
  "/updates": "Updates",
  "/terminal": "Terminal",
  "/metrics": "Metrics",
  "/services": "Services",
  "/storage": "Storage",
  "/network": "Network",
  "/processes": "Processes",
}

export function SiteHeader({ user, csrfToken }: { user: User; csrfToken: string }) {
  const location = useLocation()
  const { theme, setTheme } = useTheme()

  async function logout() {
    await api<void>("/auth/logout", { method: "POST", headers: { "X-CSRF-Token": csrfToken } })
    window.location.reload()
  }

  return (
    <header className="flex h-(--header-height) shrink-0 items-center border-b">
      <div className="flex w-full items-center gap-2 px-4 lg:px-6">
        <SidebarTrigger className="-ml-1" />
        <Separator orientation="vertical" className="mx-1 h-4 data-vertical:self-auto" />
        <h1 className="min-w-0 flex-1 truncate text-base font-medium">{titles[location.pathname] ?? "Tako"}</h1>
        <span className="hidden text-sm text-muted-foreground sm:inline">{user.username}</span>
        <Button
          variant="ghost"
          size="icon"
          onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
          aria-label="Toggle color theme"
        >
          {theme === "dark" ? <SunIcon /> : <MoonIcon />}
        </Button>
        <Button variant="ghost" size="icon" onClick={logout} aria-label="Sign out">
          <LogOutIcon />
        </Button>
      </div>
    </header>
  )
}

