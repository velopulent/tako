import type { RefreshInterval } from "@/lib/monitoring"

export type User = {
  username: string
  name: string
  uid: number
  gid: number
}

export type SessionResponse = {
  user: User
  csrfToken: string
  administrative?: boolean
  adminUntil?: string
  adminIdleTimeoutSeconds?: number
}

export type LoginPrompt = {
  id: string
  style: "hidden" | "text" | "info" | "error"
  message: string
}

export type LoginChallenge = {
  conversationId: string
  prompts: LoginPrompt[]
}

export type HostInfo = {
  hostname: string
  operatingSystem: string
  kernel: string
  architecture: string
  uptimeSeconds: number
  bootedAt: string
  bootId?: string
  hardware: {
    available: boolean
    cpuModel?: string
    cpuCores?: number
    memoryTotal?: number
    memoryAvailable?: number
    reason?: string
  }
  shutdown: {
    available: boolean
    clean: boolean
    reason?: string
  }
  restart: {
    available: boolean
    required: boolean
    source?: string
    reason?: string
  }
}

export type MetricSample = {
  timestamp: string
  cpuPercent: number
  memoryUsed: number
  memoryTotal: number
  load1: number
  load5: number
  load15: number
  cpuCorePercent: number[]
  swapUsed: number
  swapTotal: number
  networkRx: number
  networkTx: number
  diskRead: number
  diskWrite: number
  interfaces: Record<string, { rx: number; tx: number }>
}

export type DashboardResponse = {
  host: HostInfo
  metrics: MetricSample
}

export type Capability = {
  id: string
  state: "ready" | "degraded" | "unavailable" | "conflicted"
  backend?: string
  version?: string
  readable: boolean
  mutable: boolean
  rollback: boolean
  readAuthority: "none" | "session" | "user" | "administrative"
  mutationAuthority: "none" | "user" | "administrative"
  contract: string
  reason?: string
  missingDependency?: string
  setupGuidance?: string
}

export type OperationReceipt = {
  id: string
  actor: string
  target: string
  startedAt: string
  completedAt: string
  result: string
  error?: string
  administrative: boolean
}

export type DiagnosticJobState =
  "pending" | "running" | "succeeded" | "failed" | "canceled" | "interrupted"

export type DiagnosticJob = {
  id: string
  kind: "host-inventory"
  actor: string
  state: DiagnosticJobState
  progress: number
  message: string
  result?: {
    host: HostInfo
    capabilities: Capability[]
  }
  error?: string
  createdAt: string
  startedAt?: string
  completedAt?: string
  cancelRequested: boolean
  dangerous: boolean
}

export type HostConfiguration = {
  hostname: string
  timezone: string
  ntpEnabled: boolean
  fingerprint: string
}

export type HostConfigurationPreview = {
  current: HostConfiguration
  proposed: HostConfiguration
  changes: ("hostname" | "timezone" | "ntp")[]
  stale: boolean
}

export type PowerStatus = {
  available: boolean
  reboot: {
    state: "available" | "challenged" | "denied" | "inhibited" | "unavailable"
    available: boolean
    reason?: string
  }
  shutdown: {
    state: "available" | "challenged" | "denied" | "inhibited" | "unavailable"
    available: boolean
    reason?: string
  }
  inhibitors: {
    what: string
    who: string
    why: string
    mode: string
    uid: number
    pid: number
  }[]
  fingerprint: string
  reason?: string
}

export type ProcessInfo = {
  pid: number
  ppid: number
  started: number
  user: string
  program: string
  command: string
  state: string
  threads: number
  cpuTime: number
  memory: number
  virtualMemory: number
  diskRead: number
  diskWrite: number
  cpuPercent?: number
  diskReadRate?: number
  diskWriteRate?: number
  networkRxRate?: number
  networkTxRate?: number
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
  manager: string
}
export type ServiceInfo = {
  name: string
  description: string
  loadState: string
  activeState: string
  subState: string
  fileState: string
  scope: "system" | "user"
  type: "service" | "target" | "socket" | "timer" | "path"
}
export type ServiceDetail = ServiceInfo & {
  path: string
  mainPid: number
  memoryCurrent: number
  tasksCurrent: number
  activeEnterTimestamp: number
  requires: string[]
  wants: string[]
  wantedBy: string[]
  conflicts: string[]
  before: string[]
  after: string[]
}
export type ServiceImpactRelation = {
  name: string
  relationship: string
}
export type ServiceImpact = {
  scope: string
  unit: string
  action: string
  currentState: string
  currentSubState: string
  affected: ServiceImpactRelation[]
  warnings: string[]
}
export type UnitConfiguration = {
  path: string
  content: string
  truncated: boolean
}
export type LogEntry = {
  timestamp: string
  priority: string
  unit: string
  message: string
  details?: Record<string, string>
}
export type JournalPage = {
  items: LogEntry[]
  nextCursor?: string
}
export type UpdateStatus = {
  available: boolean
  backend: string
  message: string
}
export type TerminalStatus = { available: boolean; message: string }
export type MonitoringPreference = {
  defaultInterval: RefreshInterval
  revision: number
}

export type TimerAction =
  "preview" | "create" | "update" | "delete" | "enable" | "disable"

export type TimerOperation = {
  action: TimerAction
  scope: "system" | "user"
  name: string
  description?: string
  onCalendar?: string
  onBootSec?: string
  onUnitActiveSec?: string
  command?: string
  persistent?: boolean
  expectedFingerprint?: string
}

export type TimerDefinition = {
  description?: string
  onCalendar?: string
  onBootSec?: string
  onUnitActiveSec?: string
  command?: string
  persistent?: boolean
}

export type TimerState = {
  scope: "system" | "user"
  name: string
  timerUnit: string
  serviceUnit: string
  exists: boolean
  enabled: boolean
  fingerprint?: string
  definition?: TimerDefinition
}

export type ServiceOverrideOperation = {
  action: "preview" | "apply" | "delete"
  scope: "system" | "user"
  unit: string
  environment?: Record<string, string>
  restart?: string
  restartSec?: string
  timeoutStartSec?: string
  timeoutStopSec?: string
  nice?: number
  cpuQuota?: string
  memoryMax?: string
  tasksMax?: number
  expectedFingerprint?: string
}

export type ServiceOverrideDefinition = Omit<
  ServiceOverrideOperation,
  "action" | "scope" | "unit" | "expectedFingerprint"
>

export type ServiceOverrideState = {
  scope: "system" | "user"
  unit: string
  path: string
  exists: boolean
  fingerprint?: string
  definition?: ServiceOverrideDefinition
  guidance: string[]
}

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
