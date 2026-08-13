const titles: Record<string, string> = {
  dashboard: "Dashboard",
  logs: "System logs",
  users: "Users",
  updates: "Updates",
  operations: "Operations",
  jobs: "Jobs",
  terminal: "Terminal",
  metrics: "Metrics",
  services: "Services",
  storage: "Storage",
  network: "Network",
  processes: "Processes",
  settings: "Settings",
}

export function pageTitle(pathname: string) {
  const segments = pathname.split("/").filter(Boolean)
  return titles[segments[0] ?? "dashboard"] ?? "Tako"
}
