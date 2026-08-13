import type * as React from "react"
import { Link, useLocation } from "@tanstack/react-router"
import {
  ActivityIcon,
  ChartNoAxesCombinedIcon,
  FileClockIcon,
  GaugeIcon,
  HardDriveIcon,
  NetworkIcon,
  ShellIcon,
  PackageCheckIcon,
  ServerCogIcon,
  SquareTerminalIcon,
  UserRoundCogIcon,
  UsersIcon,
} from "lucide-react"

import type { User } from "@/lib/api"
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
} from "@/components/ui/sidebar"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"

const modules = [
  { title: "Dashboard", to: "/", icon: GaugeIcon },
  { title: "Logs", to: "/logs", icon: FileClockIcon },
  { title: "Users", to: "/users", icon: UsersIcon },
  { title: "Updates", to: "/updates", icon: PackageCheckIcon },
  { title: "Terminal", to: "/terminal", icon: SquareTerminalIcon },
  { title: "Metrics", to: "/metrics", icon: ChartNoAxesCombinedIcon },
  { title: "Services", to: "/services", icon: ServerCogIcon },
  { title: "Storage", to: "/storage", icon: HardDriveIcon },
  { title: "Network", to: "/network", icon: NetworkIcon },
  { title: "Processes", to: "/processes", icon: ActivityIcon },
] as const

export function AppSidebar({ user, ...props }: React.ComponentProps<typeof Sidebar> & { user: User }) {
  const location = useLocation()
  const initials = user.name
    .split(" ")
    .map((part) => part[0])
    .join("")
    .slice(0, 2)
    .toUpperCase()

  return (
    <Sidebar collapsible="icon" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" render={<Link to="/" />} tooltip="Tako">
              <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                <ShellIcon aria-hidden="true" />
              </div>
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-semibold">Tako</span>
                <span className="truncate text-xs">System console</span>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Host management</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {modules.map((item) => (
                <SidebarMenuItem key={item.to}>
                  <SidebarMenuButton
                    render={<Link to={item.to} />}
                    tooltip={item.title}
                    isActive={location.pathname === item.to}
                  >
                    <item.icon aria-hidden="true" />
                    <span>{item.title}</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" tooltip={user.username}>
              <Avatar className="size-8 rounded-lg">
                <AvatarFallback className="rounded-lg">{initials || <UserRoundCogIcon />}</AvatarFallback>
              </Avatar>
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-medium">{user.name}</span>
                <span className="truncate text-xs text-muted-foreground">{user.username}</span>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}
