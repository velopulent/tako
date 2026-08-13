export type User = {
  username: string
  name: string
  uid: number
  gid: number
}

export type SessionResponse = {
  user: User
  csrfToken: string
}

export type HostInfo = {
  hostname: string
  operatingSystem: string
  kernel: string
  architecture: string
  uptimeSeconds: number
  bootedAt: string
}

export type MetricSample = {
  timestamp: string
  cpuPercent: number
  memoryUsed: number
  memoryTotal: number
  load1: number
  networkRx: number
  networkTx: number
}

export type DashboardResponse = {
  host: HostInfo
  metrics: MetricSample
}

export type Capability = {
  id: string
  available: boolean
  reason?: string
}

export type ProcessInfo = {
  pid: number
  ppid: number
  user: string
  command: string
  state: string
  cpuTime: number
  memory: number
}
export type UserInfo = {
  username: string
  uid: number
  gid: number
  name: string
  home: string
  shell: string
  system: boolean
}
export type MountInfo = {
  source: string
  target: string
  filesystem: string
  total: number
  used: number
  available: number
  percent: number
}
export type InterfaceInfo = {
  name: string
  index: number
  mtu: number
  hardware: string
  addresses: string[]
  up: boolean
  rx: number
  tx: number
}
export type ServiceInfo = {
  name: string
  description: string
  loadState: string
  activeState: string
  subState: string
}
export type LogEntry = {
  timestamp: string
  priority: string
  unit: string
  message: string
}
export type UpdateStatus = {
  available: boolean
  backend: string
  message: string
}
export type TerminalStatus = { available: boolean; message: string }

type Problem = {
  code?: string
  detail?: string
}

export class APIError extends Error {
  readonly status: number
  readonly code?: string

  constructor(
    status: number,
    code?: string,
    message = `API request failed: ${status}`
  ) {
    super(message)
    this.name = "APIError"
    this.status = status
    this.code = code
  }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    credentials: "same-origin",
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init?.headers,
    },
  })
  if (!response.ok) {
    const problem = (await response.json().catch(() => ({}))) as Problem
    throw new APIError(response.status, problem.code, problem.detail)
  }
  if (response.status === 204) {
    return undefined as T
  }
  return response.json() as Promise<T>
}
