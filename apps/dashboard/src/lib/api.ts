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

export type BrandingResponse = {
  distribution: string
  hostname: string
  backgroundUrl?: string
}

export type FileEntry = {
  name: string
  path: string
  kind: "file" | "directory" | "symlink"
  size: number
  mode: number
  modifiedAt: string
  fingerprint: string
  hidden: boolean
  readable: boolean
  writable: boolean
  permissionDenied?: boolean
  symlinkTarget?: string
  mime?: string
  reason?: string
  previewToken?: string
}

export type FileResult = {
  uploadId?: string
  directory?: {
    parent?: string
    nextOffset?: number
    hasMore?: boolean
    path: string
    entries: FileEntry[]
    showHidden: boolean
    fingerprint: string
  }
  entry?: FileEntry
  entries?: FileEntry[]
  search?: {
    root: string
    query: string
    entries: FileEntry[]
    limited: boolean
  }
  content?: string
  offset?: number
  total?: number
  eof?: boolean
  mime?: string
  fingerprint?: string
  warnings?: string[]
  message?: string
  uploads?: {
    uploadId: string
    path: string
    offset: number
    total: number
    expiresAt: string
    completed?: boolean
  }[]
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
  disks: Record<string, DiskSample>
  interfaces: Record<string, { rx: number; tx: number }>
}

export type DashboardResponse = {
  host: HostInfo
  metrics: MetricSample
  storage?: StorageSummary
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

export type DiagnosticJobState =
  | "pending"
  | "running"
  | "succeeded"
  | "failed"
  | "canceled"
  | "interrupted"

export type DiagnosticJob = {
  id: string
  kind: "host-inventory" | "software-update"
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
  ioDenied?: boolean
  cpuPercent?: number
  diskReadRate?: number
  diskWriteRate?: number
  networkRxRate?: number
  networkTxRate?: number
  permissionDenied?: boolean
  reason?: string
}
export type ProcessSocket = {
  protocol: string
  local: string
  remote?: string
  state?: string
}
export type ProcessResourceSample = {
  timestamp: string
  cpuTime: number
  memory: number
  virtualMemory: number
  diskRead: number
  diskWrite: number
  ioDenied?: boolean
}
export type ProcessDetails = {
  process: ProcessInfo
  parent?: ProcessInfo
  children: ProcessInfo[]
  cgroup?: string
  openFiles: string[]
  sockets: ProcessSocket[]
  history: ProcessResourceSample[]
  accessIssues?: string[]
}
export type ProcessTarget = { pid: number; started: number }
export type SignalTarget = ProcessTarget & {
  uid: number
  user: string
  program: string
}
export type SignalPreview = {
  signal: string
  tree: boolean
  targets: SignalTarget[]
  fingerprint: string
}
export type SignalResult = {
  signal: string
  tree: boolean
  targets: SignalTarget[]
  signaled: SignalTarget[]
  failures?: { pid: number; error: string }[]
}
export type UserInfo = {
  username: string
  uid: number
  gid: number
  name: string
  home: string
  shell: string
  system: boolean
  groups: string[]
  source: "local" | "nss-read-only"
  local: boolean
  mutable: boolean
  reason?: string
}
export type GroupInfo = {
  name: string
  gid: number
  members: string[]
  source: "local" | "nss-read-only"
  local: boolean
  mutable: boolean
  reason?: string
}
export type LocalAccountOperation = {
  action: "create" | "update" | "lock" | "unlock" | "delete"
  username: string
  name?: string
  home?: string
  shell?: string
  expectedFingerprint?: string
  confirmation?: string
}
export type LocalAccountState = {
  username: string
  exists: boolean
  user?: UserInfo
  locked: boolean
  fingerprint: string
  source?: "local" | "nss-read-only"
}
export type LocalAccountPreview = {
  action: LocalAccountOperation["action"]
  username: string
  current: LocalAccountState
  changes: string[]
  warnings: string[]
  stale: boolean
  allowed: boolean
  reason?: string
  requiresConfirmation: boolean
}
export type PasswordChangeOperation =
  | {
      action: "change"
      currentPassword: string
      newPassword: string
      confirmation: string
    }
  | {
      action: "reset"
      username: string
      newPassword: string
      confirmation: string
    }
export type SSHKey = {
  fingerprint: string
  type: string
  comment?: string
  line: string
}
export type SSHKeyState = {
  username: string
  path: string
  fingerprint: string
  keys: SSHKey[]
  writable: boolean
  authority: "user" | "administrative"
  reason?: string
}
export type SSHKeyOperation = {
  action: "add" | "remove"
  username: string
  key?: string
  fingerprint?: string
  expectedFingerprint?: string
  confirmation?: string
}
export type SSHKeyPreview = {
  action: SSHKeyOperation["action"]
  username: string
  current: SSHKeyState
  changes: string[]
  warnings: string[]
  stale: boolean
  allowed: boolean
  reason?: string
  requiresConfirmation: boolean
}
export type GroupMembershipOperation = {
  action: "add" | "remove"
  username: string
  group: string
  expectedFingerprint?: string
}
export type GroupMembershipState = {
  username: string
  group: GroupInfo
  member: boolean
  fingerprint: string
}
export type GroupMembershipPreview = {
  action: GroupMembershipOperation["action"]
  username: string
  group: string
  current: GroupMembershipState
  changes: string[]
  warnings: string[]
  stale: boolean
  allowed: boolean
  reason?: string
  requiresConfirmation: boolean
}
export type AdministrativeRoleOperation = {
  action: "grant" | "revoke"
  username: string
  role: "administrator"
  expectedFingerprint?: string
  confirmation?: string
}
export type AdministrativeRoleState = {
  username: string
  role: "administrator"
  group: string
  member: boolean
  members: string[]
  fingerprint: string
}
export type AdministrativeRolePreview = {
  action: AdministrativeRoleOperation["action"]
  username: string
  role: "administrator"
  current: AdministrativeRoleState
  changes: string[]
  warnings: string[]
  stale: boolean
  allowed: boolean
  reason?: string
  requiresConfirmation: boolean
}
export type LocalGroupOperation = {
  action: "create" | "delete"
  group: string
  expectedFingerprint?: string
  confirmation?: string
}
export type LocalGroupState = {
  group: string
  exists: boolean
  groupInfo?: GroupInfo
  fingerprint: string
  source?: "local" | "nss-read-only"
}
export type LocalGroupPreview = {
  action: LocalGroupOperation["action"]
  group: string
  current: LocalGroupState
  changes: string[]
  warnings: string[]
  stale: boolean
  allowed: boolean
  reason?: string
  requiresConfirmation: boolean
}
export type MountPoint = {
  target: string
  root?: string
  readOnly?: boolean
}
export type FileSystemInfo = {
  device: string
  type: string
  majorMinor: string
  network: boolean
  readOnly: boolean
  total: number
  used: number
  available: number
  percent: number
  targets: MountPoint[]
}
export type StoragePartition = {
  path: string
  name?: string
  size: number
  filesystem?: string
  label?: string
  uuid?: string
  parent?: string
  readOnly: boolean
  mountPoints: MountPoint[]
}
export type StorageDevice = {
  path: string
  name: string
  type: string
  model?: string
  serial?: string
  transport?: string
  size: number
  readOnly: boolean
  removable: boolean
  partitions: StoragePartition[]
  smart?: {
    available: boolean
    passed?: boolean
    temperatureC?: number
    powerOnHours?: number
    failing?: boolean
    reason?: string
  }
  nvme?: {
    available: boolean
    temperatureC?: number
    percentageUsed?: number
    criticalWarning?: number
    reason?: string
  }
}
export type StorageSnapshot = {
  filesystems: FileSystemInfo[]
  devices: StorageDevice[]
  fingerprint: string
  readOnly: boolean
  reason?: string
}
export type StorageInventoryResponse = {
  items: FileSystemInfo[]
  devices: StorageDevice[]
  fingerprint: string
  readOnly: boolean
  reason?: string
}
export type StorageOperation = {
  action:
    | "preview"
    | "mount"
    | "unmount"
    | "persistent-mount"
    | "persistent-unmount"
  device: string
  target?: string
  filesystem?: string
  options?: string[]
  expectedFingerprint?: string
  confirmation?: string
}
export type StorageState = {
  snapshot: StorageSnapshot
  action: string
  applied: boolean
  warning?: string
}
export type StorageSummary = {
  filesystems: number
  total: number
  used: number
  percent: number
}
export type DiskSample = {
  read: number
  write: number
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
  profile?: string
  owner?: string
  conflict?: boolean
  reason?: string
}
export type NetworkAddress = {
  interface: string
  address: string
  family: string
  scope?: string
}
export type NetworkRoute = {
  destination: string
  gateway?: string
  device?: string
  metric?: number
}
export type NetworkOwnership = {
  activeOwner: string
  detected: string[]
  conflicted: boolean
  reason?: string
}
export type NetworkResponse = {
  items: InterfaceInfo[]
  addresses?: NetworkAddress[]
  routes?: NetworkRoute[]
  dns?: string[]
  ownership?: NetworkOwnership
  fingerprint?: string
}
export type NetworkOperation = {
  backend: "NetworkManager" | "Netplan" | "systemd-networkd" | "ifupdown"
  action:
    | "preview"
    | "dhcp"
    | "static"
    | "dns"
    | "route-add"
    | "route-remove"
    | "checkpoint"
    | "commit"
    | "rollback"
  interface?: string
  connection?: string
  address?: string
  addresses?: string[]
  gateway?: string
  dns?: string[]
  route?: string
  metric?: number
  expectedFingerprint?: string
  confirmation?: string
  reconnectToken?: string
  checkpoint?: string
  ipv4Method?: string
  ipv4Address?: string
  ipv4Gateway?: string
  ipv6Method?: string
  ipv6Address?: string
  ipv6Gateway?: string
}
export type NetworkState = {
  snapshot: NetworkResponse
  action: string
  checkpoint?: string
  committed: boolean
  rollback: boolean
  reconnectRequired?: boolean
  reconnectToken?: string
  rollbackDeadline?: string
  warning?: string
}
export type FirewallSnapshot = {
  backend: string
  active: boolean
  version?: string
  defaultZone?: string
  persistentDefaultZone?: string
  zones: string[]
  rules: string[]
  runtimeRules?: string[]
  persistentRules?: string[]
  synchronized: boolean
  conflicted: boolean
  readOnly: boolean
  reason?: string
  fingerprint: string
}
export type FirewallOperation = {
  backend: "auto" | "firewalld" | "UFW"
  action:
    | "preview"
    | "enable"
    | "disable"
    | "default-zone"
    | "add-service"
    | "remove-service"
    | "add-port"
    | "remove-port"
    | "add-source"
    | "remove-source"
    | "reload"
    | "commit"
    | "rollback"
  zone?: string
  service?: string
  port?: string
  source?: string
  defaultZone?: string
  expectedFingerprint?: string
  confirmation?: string
  persist?: boolean
  rollbackSeconds?: number
  checkpoint?: string
  rollbackToken?: string
}
export type FirewallState = {
  snapshot: FirewallSnapshot
  action: string
  applied: boolean
  committed?: boolean
  rollbackRequired?: boolean
  checkpoint?: string
  rollbackToken?: string
  rollbackDeadline?: string
  warning?: string
}
export type SecurityFinding = {
  framework: string
  kind: string
  subject: string
  message: string
  severity: string
  guidance?: string
}
export type SecurityStatus = {
  selinux: {
    kernelPresent: boolean
    userspace: boolean
    mode: string
    policy?: string
    booleans: string[]
    denials: string[]
  }
  apparmor: {
    kernelPresent: boolean
    userspace: boolean
    profiles: string[]
    denials: string[]
  }
  active: string
  findings: SecurityFinding[]
  fingerprint: string
}
export type IncidentEvent = {
  id: string
  timestamp: string
  severity: "critical" | "error" | "warning"
  kind: string
  source?: string
  summary: string
  cursor?: string
}
export type IncidentTimeline = {
  items: IncidentEvent[]
  since: string
  until: string
  partial: boolean
  warnings?: string[]
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
  id?: string
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
export type LoginHistoryIdentity = {
  username: string
  source: "local" | "nss-read-only" | "deleted-unknown"
  present: boolean
}
export type LoginHistoryEntry = {
  timestamp: string
  event: "login" | "session-open" | "session-close"
  outcome: "success" | "failure"
  service: string
  remote?: string
  session?: string
}
export type LoginHistoryPage = {
  identity: LoginHistoryIdentity
  items: LoginHistoryEntry[]
  nextCursor?: string
}
export type SavedLogFilter = {
  boot?: string
  since?: string
  until?: string
  priority?: string
  unit?: string
  executable?: string
  text?: string
  details?: boolean
}
export type SavedLogView = {
  id: string
  name: string
  filter: SavedLogFilter
  createdAt: string
  updatedAt: string
}
export type UpdateStatus = {
  available: boolean
  backend: string
  version?: string
  contract: string
  packages: UpdatePackage[]
  fingerprint: string
  externalLock: boolean
  lockReason?: string
  message: string
  reason?: string
  recovery?: UpdateRecovery
  lastChecked?: string
  timeSinceRefresh?: number
}
export type UpdateRecovery = {
  authoritative: boolean
  rebootRequired: boolean
  restartServices: string[]
  rebootPackages?: string[]
  manualPackages?: string[]
  hints: string[]
  source: string
  reason?: string
}
export type UpdateOperation = {
  expectedFingerprint: string
  confirmed: boolean
  riskAccepted?: boolean
}
export type UpdateChange = {
  action: "install" | "upgrade" | "remove" | "downgrade" | "replace" | string
  name: string
  architecture?: string
  currentVersion?: string
  candidateVersion?: string
  currentRepository?: string
  targetRepository?: string
  currentVendor?: string
  targetVendor?: string
}
export type UpdatePreview = {
  current: UpdateStatus
  changes: UpdateChange[]
  warnings: string[]
  fingerprint: string
  stale: boolean
  allowed: boolean
  requiresConfirmation: boolean
  requiresRiskConfirmation: boolean
  reason?: string
}
export type UpdateResult = {
  backend: string
  changes: UpdateChange[]
  verified: boolean
  message: string
  fingerprint: string
  recovery?: UpdateRecovery
}
export type UpdatePackage = {
  name: string
  architecture?: string
  currentVersion?: string
  candidateVersion: string
  severity?: string
  secSeverity?: "critical" | "important" | "moderate" | "low"
  size?: number
  summary?: string
  details?: string
  advisoryId?: string
  cveUrls?: string[]
  bugUrls?: string[]
  vendorUrls?: string[]
  description?: string
  markdown?: boolean
  groupKey?: string
  dependencies?: string[]
}
export type UpdateHistoryEntry = {
  time: number
  packages: Record<string, string>
}
export type UpdateProgress = {
  sequence: number
  jobId?: string
  active: boolean
  phase: string
  package?: string
  current: number
  total: number
  percent: number
  message: string
  cancelable: boolean
  timestamp: string
}
export type UpdateOutput = {
  sequence: number
  jobId?: string
  stream: "stdout" | "stderr" | string
  line: string
  timestamp: string
}
export type UpdateObservation = {
  progress: UpdateProgress
  output: UpdateOutput[]
}
export type KpatchStatus = {
  supported: boolean
  loaded: string[]
  installed: string[]
}
export type KpatchSettings = {
  supported: boolean
  missing: string[]
  unavailable: string[]
  auto: boolean
  serviceEnabled: boolean
  kernel?: string
  patchName?: string
  patchInstalled: boolean
  patchUnavailable: boolean
}
export type KpatchOperation = {
  apply: boolean
  currentOnly?: boolean
}
export type TerminalStatus = { available: boolean; message: string }
export type MonitoringPreference = {
  defaultInterval: RefreshInterval
}

export type TimerAction =
  | "preview"
  | "create"
  | "update"
  | "delete"
  | "enable"
  | "disable"

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
