export type Board = {
  id: string;
  name: string;
  host: string;
  user: string;
  port: number;
  identityFile?: string;
};

export type Capabilities = {
  protocolMin: number;
  protocolMax: number;
  eventSchema: number;
  capabilities: string[];
  maximumActiveTasks: number;
  maximumRetainedTasks: number;
  sandbox?: {available: boolean; backend?: string; profiles?: string[]; filesystemWritesRestricted: boolean; devicesRestricted: boolean; capabilitiesDropped: boolean; networkRestricted: boolean; reason?: string};
};

export type ExtensionEntry = {id: string; name: string; version: string; kind: 'extension' | 'skill' | 'provider' | 'integration'; description: string; origin: string; scope: string; runtime: string; entrypoint: string; trust: string; defaultEnabled: boolean; required: boolean; provides: string[]; requires: string[]; permissions: string[]; targets: string[]; status?: 'included' | 'configured' | 'available' | 'missing' | 'disabled'; statusDetail?: string};
export type ExtensionDiagnostic = {source: string; status: string; message: string};
export type ExtensionCatalog = {schemaVersion: number; apiVersion: string; productVersion: string; hostVersion: string; capturedAt?: string; entries: ExtensionEntry[]; diagnostics?: ExtensionDiagnostic[]; policy: {inventoryOnly: boolean; executionAuthority: string; permissionAuthority: string; thirdPartyRuntime: string; hotReload: boolean}};

export type DaemonInfo = {
  version: string;
  pid: number;
  startedAt: string;
  activeTasks: number;
  queuedTasks?: number;
  maximumTasks: number;
  stateRoot: string;
  configurationCurrent?: boolean;
  build?: {status: 'verified' | 'invalid' | 'unavailable'; reason?: string; commit?: string; dirty?: boolean; builtAt?: string; target?: string; binarySha256?: string; piVersion?: string; piCommit?: string};
};

export type CompatibilityIssue = {code: string; severity: 'warning' | 'error'; message: string; action?: string};
export type ConnectionCompatibility = {
  status: 'supported' | 'limited' | 'upgrade-required';
  summary: string;
  appVersion: string;
  agentdVersion: string;
  protocol: number;
  eventSchema: number;
  boardId?: string;
  rdkOsVersion?: string;
  validatedTarget: boolean;
  issues: CompatibilityIssue[];
};

export type Connection = {
  board: Board;
  connected: boolean;
  reconnected?: boolean;
  daemon?: DaemonInfo;
  capabilities?: Capabilities;
  snapshot?: SystemSnapshot;
  compatibility?: ConnectionCompatibility;
  error?: string;
};

export type ThermalZone = {name: string; celsius: number};
export type BPUCoreInfo = {index: number; name: string; utilizationPercent: number; currentFrequencyHz?: number; minimumFrequencyHz?: number; maximumFrequencyHz?: number};
export type BPUTelemetryInfo = {status: 'available' | 'device-not-detected' | 'metrics-not-exposed' | 'read-failed'; source?: string};
export type AIMemoryHeapInfo = {name: string; capacityBytes?: number; allocatedBytes: number; orphanedBytes?: number};
export type AIMemoryInfo = {available: boolean; bpuAllocationAvailable: boolean; ionAvailable: boolean; cmaAvailable: boolean; dmaBufAvailable: boolean; bpuAllocatedBytes?: number; ionAllocatedBytes?: number; ionOrphanedBytes?: number; cmaTotalBytes?: number; cmaFreeBytes?: number; dmaBufBytes?: number; dmaBufObjects?: number; heaps?: AIMemoryHeapInfo[]};
export type AcceleratorMemoryPoolInfo = {name: string; totalBytes: number; usedBytes: number; freeBytes: number; processBytes?: number; systemBytes?: number};
export type AcceleratorProcessInfo = {pid: number; name: string; rssBytes: number; hbmemBytes: number};
export type AcceleratorInfo = {available: boolean; source?: string; capturedAt?: string; ddrReadMiBps?: number; ddrWriteMiBps?: number; hbmemPools?: AcceleratorMemoryPoolInfo[]; processes?: AcceleratorProcessInfo[]};
export type HardwareLease = {resource: string; taskId: string; pid: number; cwd?: string; acquiredAt: string};
export type WorkspaceWriteLease = {taskId: string; pid: number; cwd: string; acquiredAt: string};
export type SystemSnapshot = {
  capturedAt: string;
  board: string;
  boardId: string;
  hostname: string;
  rdkOsVersion: string;
  kernel: string;
  architecture: string;
  cpuCores: number;
  loadAverage: number[];
  memory: {totalBytes: number; availableBytes: number};
  disk: {path: string; totalBytes: number; availableBytes: number};
  thermalZones: ThermalZone[];
  bpuDevices: string[];
  bpuCores?: BPUCoreInfo[];
  bpuTelemetry?: BPUTelemetryInfo;
  aiMemory?: AIMemoryInfo;
  accelerator?: AcceleratorInfo;
  hardwareLeases?: HardwareLease[];
  workspaceWrites?: WorkspaceWriteLease[];
  rdkUtilities: Record<string, boolean>;
  uptimeSeconds: number;
};

export type SupportBundle = {id: string; createdAt: string; path: string; sizeBytes: number; sha256: string; excluded: string[]; checks: {pass: number; warn: number; fail: number}};

export type DeploymentArtifact = {path: string; relativePath: string; name: string; kind: string; sizeBytes: number; modifiedAt: string; compatibility: 'candidate' | 'unverified' | 'conversion-required' | 'mismatch'; reason: string};
export type DeploymentInspection = {capturedAt: string; cwd: string; board: string; boardId: string; rdkOsVersion: string; artifacts: DeploymentArtifact[]; truncated: boolean};
export type DeploymentAcceptance = {profile: string; dataset?: string; minimumAccuracySamples: number; metrics?: {name: string; unit: string; threshold: number; comparator: '<=' | '>='}[]; minimumWarmupIterations: number; minimumMeasuredIterations: number; maximumModelP95LatencyMs?: number; maximumEndToEndP95LatencyMs?: number; minimumThroughput: number; maximumTemperatureC: number; minimumMemoryAvailableBytes: number};
export type DeploymentRecord = {schema: number; cwd: string; board: string; boardId: string; rdkOsVersion: string; goal: string; artifact: DeploymentArtifact; reportPath: string; createdAt: string; acceptance?: DeploymentAcceptance};
export type DeploymentMetric = {name: string; unit: string; value: number; threshold: number; comparator: '<=' | '>='; passed: boolean};
export type DeploymentResourceSample = {capturedAt?: string; aiAllocationAvailable?: boolean; aiAllocationSource?: string; aiAllocatedBytes?: number; bpuUtilizationAvailable?: boolean; temperatureAvailable?: boolean; systemMemoryUsedBytes?: number; systemMemoryAvailableBytes?: number; ionAllocatedBytes?: number; bpuUtilizationPercent?: number; cpuLoadPercent?: number; maxTemperatureC?: number};
export type DeploymentReport = {schema: number; outcome: string; boardId: string; artifactPath: string; artifactSha256?: string; summary: string; correctness: {passed: boolean; method?: string; dataset?: string; sampleCount?: number; referenceArtifact?: string; metrics?: DeploymentMetric[]}; performance: {warmupIterations?: number; iterations?: number; p50LatencyMs?: number; p95LatencyMs?: number; throughput?: number; endToEndP50Ms?: number; endToEndP95Ms?: number}; resources?: {sampleCount?: number; baseline?: DeploymentResourceSample; peak?: DeploymentResourceSample; final?: DeploymentResourceSample; limits?: {maxTemperatureC?: number; minSystemMemoryAvailableBytes?: number}}};
export type DeploymentStatus = {taskId: string; phase: string; deployment: DeploymentRecord; report?: DeploymentReport; issue?: string};
export type StartDeploymentRequest = {cwd: string; artifactPath: string; goal: 'deploy-and-validate' | 'benchmark'; name?: string; model?: string; permissionMode?: string; sandboxMode?: 'system' | 'off'; profile?: string};

export type Approval = {
  id: string;
  method: 'confirm' | 'select' | 'input' | 'editor';
  title?: string;
  message?: string;
  options?: string[];
  placeholder?: string;
  prefill?: string;
  active: boolean;
};

export type TurnWorkspaceEvidence = {status: 'captured' | 'partial' | 'not-repository' | 'unavailable'; capturedAt: string; stateDigest?: string; dirty?: boolean; changedFiles?: number; truncated?: boolean};
export type TaskTurnEvidence = {
  turn: number;
  status: 'running' | 'completed' | 'interrupted' | 'failed' | 'stopped';
  evidence: 'in-progress' | 'complete' | 'partial';
  startedAt: string;
  endedAt?: string;
  startSequence: number;
  endSequence?: number;
  toolsStarted: number;
  toolsCompleted: number;
  toolsFailed: number;
  openTools: number;
  workspaceBefore?: TurnWorkspaceEvidence;
  workspaceAfter?: TurnWorkspaceEvidence;
  workspaceChanged?: boolean;
  recommendedAction: 'none' | 'review' | 'review-before-resume' | 'review-before-restart';
};

export type Task = {
  id: string;
  name: string;
	cwd: string;
	projectCwd?: string;
	workspaceMode?: 'shared' | 'worktree';
	workspaceId?: string;
	worktreePath?: string;
	worktreeBase?: string;
  status: string;
  pid?: number;
  createdAt: string;
  updatedAt: string;
  lastSequence: number;
  lastError?: string;
  failure?: {code: string; message: string; recovery: 'resume' | 'restart' | 'check-model' | 'diagnose' | 'none'};
  logTruncated?: boolean;
  sessionFile?: string;
  sessionId?: string;
  approved?: boolean;
  resumeCount?: number;
  restartCount?: number;
  model?: string;
  permissionMode?: 'review' | 'ask' | 'developer';
  sandboxMode?: 'review' | 'workspace' | 'system' | 'off';
  sandbox?: {requested: string; effective: string; backend: string; filesystemRestricted: boolean; devicesRestricted: boolean; capabilitiesDropped: boolean; networkRestricted: boolean; reason?: string};
  parentTaskId?: string;
  forkSequence?: number;
  branchKind?: 'side' | 'edit';
  awaitingPrompt?: boolean;
  queuedAt?: string;
  queueOperation?: 'start' | 'fork' | 'resume' | 'restart';
  archivedAt?: string;
  pendingApprovals?: Approval[];
  deployment?: DeploymentRecord;
  turnEvidence?: TaskTurnEvidence[];
};

export type NormalizedEvent = {
  schema: number;
  type: string;
  data?: Record<string, unknown>;
  item?: {id?: string; type: string; status: string};
};

export type TaskEvent = {
  protocol: number;
  kind: string;
  taskId: string;
  sequence: number;
  time: string;
  event: Record<string, unknown>;
  normalized?: NormalizedEvent;
};

export type TaskPage = { tasks: Task[]; nextCursor?: string };
export type EventPage = { events: TaskEvent[]; nextAfter?: number; hasMore: boolean };
export type EventEnvelope = { boardId: string; event: TaskEvent };
export type ModelOption = {
  provider: string;
  id: string;
  name: string;
  default?: boolean;
  capabilities?: {reasoning: boolean; imageInput: boolean};
  capabilitySource?: 'runtime-model-table' | 'conservative-default' | string;
};
export type ModelHealth = {
  provider: string;
  model: string;
  status: 'available' | 'unavailable';
  category: string;
  message: string;
  transport?: 'sse' | 'json' | string;
  checkedAt: string;
  expiresAt: string;
  firstByteMs?: number;
  latencyMs?: number;
  attempts: number;
  cached: boolean;
};

export type ModelConformanceCheck = {name: string; status: 'passed' | 'degraded' | 'failed' | 'blocked' | 'skipped'; category: string; message: string; latencyMs?: number};
export type ModelConformance = {
  provider: string;
  model: string;
  status: 'verified' | 'compatible' | 'failed';
  message: string;
  checkedAt: string;
  expiresAt: string;
  durationMs?: number;
  attempts: number;
  cached: boolean;
  checks: ModelConformanceCheck[];
};
export type ImageContent = {type: 'image'; data: string; mimeType: string; name?: string};
export type AttachmentSummary = {name?: string; mimeType: string};
export type WorkspaceEntry = {name: string; path: string};
export type WorkspaceListing = {path: string; parent?: string; home: string; directories: WorkspaceEntry[]};
export type WorkspaceChangeFile = {path: string; originalPath?: string; status: string; kind: string; staged?: boolean; unstaged?: boolean; untracked?: boolean; conflict?: boolean};
export type WorkspaceChanges = {capturedAt: string; available: boolean; repository: boolean; repositoryRoot?: string; scope?: string; head?: string; files: WorkspaceChangeFile[]; patch?: string; filesTruncated?: boolean; patchTruncated?: boolean};
export type WorkspaceIsolation = {capturedAt: string; available: boolean; repository: boolean; eligible: boolean; recommendedMode: 'shared' | 'worktree'; repositoryRoot?: string; scope?: string; head?: string; clean: boolean; reason: string};
export type WorkspaceDelivery = {taskId: string; ready: boolean; reason: string; patchBytes?: number; digest?: string; alreadyApplied?: boolean};
export type WorkspaceApplyResult = {taskId: string; applied: boolean; staged: boolean; patchBytes: number; digest: string; appliedAt: string};
export type ForkTaskRequest = {taskId: string; sequence?: number; prompt?: string; images?: ImageContent[]; name?: string; kind: 'side' | 'edit'; model?: string; permissionMode?: string; sandboxMode?: 'review' | 'workspace' | 'system' | 'off'};
