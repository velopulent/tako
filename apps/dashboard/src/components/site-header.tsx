import { useLocation } from "@tanstack/react-router"
import {
  KeyRoundIcon,
  LockKeyholeIcon,
  LockKeyholeOpenIcon,
  MonitorIcon,
  MoonIcon,
  SunIcon,
} from "lucide-react"
import * as React from "react"

import { useAdminAccess } from "@/components/admin-access"
import { type Theme, useTheme } from "@/components/theme-provider"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Separator } from "@/components/ui/separator"
import { SidebarTrigger } from "@/components/ui/sidebar"
import { pageTitle } from "@/lib/page-title"

function useMinutesLeft(until?: Date) {
  const [now, setNow] = React.useState(() => Date.now())
  React.useEffect(() => {
    if (!until) return
    const timer = window.setInterval(() => setNow(Date.now()), 15_000)
    return () => window.clearInterval(timer)
  }, [until])
  return until ? Math.max(0, Math.ceil((until.getTime() - now) / 60_000)) : 0
}

function AdminControl() {
  const { administrative, until, request, drop } = useAdminAccess()
  const minutes = useMinutesLeft(until)
  if (!administrative) {
    return (
      <Button variant="outline" size="sm" onClick={request}>
        <LockKeyholeIcon data-icon="inline-start" />
        <span className="hidden sm:inline">Limited access</span>
        <span className="sr-only sm:hidden">Unlock administrative access</span>
      </Button>
    )
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="secondary" size="sm" />}>
        <LockKeyholeOpenIcon data-icon="inline-start" />
        <span className="hidden sm:inline">Administrator</span>
        <span className="tabular-nums text-muted-foreground">{minutes}m</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuGroup>
          <DropdownMenuLabel>
            Administrative access ends in {minutes} minute
            {minutes === 1 ? "" : "s"} unless you stay active.
          </DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem onClick={() => void drop()}>
            <KeyRoundIcon />
            Drop administrative access
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function ThemeMenu() {
  const { theme, setTheme } = useTheme()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="ghost" size="icon" />}>
        <SunIcon className="scale-100 rotate-0 transition-transform dark:scale-0 dark:-rotate-90" />
        <MoonIcon className="absolute scale-0 rotate-90 transition-transform dark:scale-100 dark:rotate-0" />
        <span className="sr-only">Theme</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuGroup>
          <DropdownMenuRadioGroup
            value={theme}
            onValueChange={(value) => setTheme(value as Theme)}
          >
            <DropdownMenuRadioItem value="light">
              <SunIcon /> Light
            </DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="dark">
              <MoonIcon /> Dark
            </DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="system">
              <MonitorIcon /> System
            </DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function SiteHeader() {
  const location = useLocation()
  return (
    <header className="sticky top-0 z-10 flex h-(--header-height) shrink-0 items-center border-b bg-background/80 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="flex w-full items-center gap-2 px-4 lg:px-6">
        <SidebarTrigger className="-ml-1" />
        <Separator
          orientation="vertical"
          className="mx-1 h-4 data-vertical:self-auto"
        />
        <h1 className="min-w-0 flex-1 truncate text-sm font-medium">
          {pageTitle(location.pathname)}
        </h1>
        <AdminControl />
        <ThemeMenu />
      </div>
    </header>
  )
}
