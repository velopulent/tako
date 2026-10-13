import { Link, useLocation } from "@tanstack/react-router"
import {
  ActivityIcon,
  ChartNoAxesCombinedIcon,
  ChevronsUpDownIcon,
  FileClockIcon,
  FilesIcon,
  GaugeIcon,
  HardDriveIcon,
  LogOutIcon,
  NetworkIcon,
  PackageCheckIcon,
  ServerCogIcon,
  ServerIcon,
  SettingsIcon,
  ShieldCheckIcon,
  SirenIcon,
  SquareTerminalIcon,
  TimerIcon,
  UsersIcon,
} from "lucide-react"
import type * as React from "react"

import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  useSidebar,
} from "@/components/ui/sidebar"
import { api } from "@/lib/api"
import { useSession } from "@/lib/session"

const groups = [
  {
    label: "Overview",
    items: [
      { title: "Dashboard", to: "/", icon: GaugeIcon },
      { title: "Metrics", to: "/metrics", icon: ChartNoAxesCombinedIcon },
      { title: "Incidents", to: "/incidents", icon: SirenIcon },
    ],
  },
  {
    label: "System",
    items: [
      { title: "Services", to: "/services", icon: ServerCogIcon },
      { title: "Timers", to: "/timers", icon: TimerIcon },
      { title: "Processes", to: "/processes", icon: ActivityIcon },
      { title: "Logs", to: "/logs", icon: FileClockIcon },
      { title: "Terminal", to: "/terminal", icon: SquareTerminalIcon },
    ],
  },
  {
    label: "Resources",
    items: [
      { title: "Storage", to: "/storage", icon: HardDriveIcon },
      { title: "Network", to: "/network", icon: NetworkIcon },
      { title: "Files", to: "/files", icon: FilesIcon },
    ],
  },
  {
    label: "Administration",
    items: [
      { title: "Accounts", to: "/accounts", icon: UsersIcon },
      { title: "Updates", to: "/updates", icon: PackageCheckIcon },
      { title: "Security", to: "/security", icon: ShieldCheckIcon },
      { title: "Host", to: "/host", icon: ServerIcon },
    ],
  },
] as const

function initialsOf(name: string) {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .map((part) => part[0])
    .join("")
    .slice(0, 2)
    .toUpperCase()
}

function isActive(pathname: string, to: string) {
  return pathname === to || (to !== "/" && pathname.startsWith(`${to}/`))
}

function UserMenu() {
  const session = useSession()
  const { isMobile, setOpenMobile } = useSidebar()
  const user = session.user
  async function signOut() {
    await api<void>("/auth/logout", { method: "POST" }).catch(() => undefined)
    window.location.assign("/")
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <SidebarMenuButton
            size="lg"
            className="data-popup-open:bg-sidebar-accent"
          />
        }
      >
        <Avatar className="size-8 rounded-lg">
          <AvatarFallback className="rounded-lg">
            {initialsOf(user.name || user.username)}
          </AvatarFallback>
        </Avatar>
        <div className="grid flex-1 text-left text-sm leading-tight">
          <span className="truncate font-medium">
            {user.name || user.username}
          </span>
          <span className="truncate text-xs text-muted-foreground">
            {user.username}
          </span>
        </div>
        <ChevronsUpDownIcon className="ml-auto" />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        side={isMobile ? "bottom" : "right"}
        align="end"
        className="w-56"
      >
        <DropdownMenuGroup>
          <DropdownMenuLabel className="font-normal">
            Signed in as <span className="font-medium">{user.username}</span>
          </DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem
            render={<Link to="/settings" />}
            onClick={() => setOpenMobile(false)}
          >
            <SettingsIcon />
            Settings
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => void signOut()}>
            <LogOutIcon />
            Sign out
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function AppSidebar(props: React.ComponentProps<typeof Sidebar>) {
  const { pathname } = useLocation()
  const { setOpenMobile } = useSidebar()
  return (
    <Sidebar collapsible="icon" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              size="lg"
              render={<Link to="/" />}
              tooltip="Tako"
            >
              <img src="/favicon.svg" alt="" className="size-8 rounded-lg" />
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-semibold">Tako</span>
                <span className="truncate text-xs text-muted-foreground">
                  Server console
                </span>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        {groups.map((group) => (
          <SidebarGroup key={group.label}>
            <SidebarGroupLabel>{group.label}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {group.items.map((item) => (
                  <SidebarMenuItem key={item.to}>
                    <SidebarMenuButton
                      render={<Link to={item.to} />}
                      tooltip={item.title}
                      isActive={isActive(pathname, item.to)}
                      onClick={() => setOpenMobile(false)}
                    >
                      <item.icon />
                      <span>{item.title}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>
      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <UserMenu />
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}
