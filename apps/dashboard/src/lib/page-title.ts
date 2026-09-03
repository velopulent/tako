const titles: Record<string, string> = {
  dashboard: "Dashboard",
  logs: "System logs",
  accounts: "Accounts",
  users: "Accounts",
  updates: "Updates",
  jobs: "Jobs",
  host: "Host",
  terminal: "Terminal",
  metrics: "Metrics",
  services: "Services",
  timers: "Timers",
  storage: "Storage",
  network: "Network",
  files: "Files",
  security: "Security",
  incidents: "Incidents",
  processes: "Processes",
  settings: "Settings",
}

export function pageTitle(pathname: string) {
  const segments = pathname.split("/").filter(Boolean)
  return titles[segments[0] ?? "dashboard"] ?? "Tako"
}
