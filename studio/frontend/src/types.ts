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
  maximumSideTasks?: number;
  maximumRetainedTasks: number;
  sandbox?: {available: boolean; backend?: string; profiles?: string[]; networkModes?: string[]; filesystemWritesRestricted: boolean; devicesRestricted: boolean; capabilitiesDropped: boolean; networkRestricted: boolean; reason?: string};
};

export type ExtensionEntry = {id: string; name: string; version: string; kind: 'extension' | 'skill' | 'provider' | 'integration' | 'package' | 'prompt' | 'theme'; resourceType?: 'extension' | 'skill' | 'package' | 'prompt' | 'theme'; description: string; origin: string; scope: string; runtime: string; entrypoint: string; trust: string; defaultEnabled: boolean; required: boolean; provides: string[]; requires: string[]; permissions: string[]; targets: string[]; status?: 'included' | 'configured' | 'available' | 'missing' | 'disabled' | 'declared' | 'discovered'; statusDetail?: string};
export type ExtensionDiagnostic = {source: string; status: string; message: string};
export type ExtensionCatalog = {schemaVersion: number; apiVersion: string; productVersion: string; hostVersion: string; capturedAt?: string; entries: ExtensionEntry[]; diagnostics?: ExtensionDiagnostic[]; policy: {inventoryOnly: boolean; executionAuthority: string; permissionAuthority: string; thirdPartyRuntime: string; hotReload: boolean}};

export type DaemonInfo = {
  version: string;
  pid: number;
  startedAt: string;
  activeTasks: number;
  queuedTasks?: number;
  maximumTasks: number;
  maximumSideTasks?: number;
  stateRoot: string;
  configurationCurrent?: boolean;
  build?: {status: 'verified' | 'invalid' | 'unavailable'; reason?: string; commit?: string; dirty?: boolean; builtAt?: string; target?: string; binarySha256?: string; piVersion?: string; piCommit?: string; piCompatibilitySha256?: string};
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
  notInstalled?: boolean;
  daemon?: DaemonInfo;
  capabilities?: Capabilities;
  snapshot?: SystemSnapshot;
  compatibility?: ConnectionCompatibility;
  error?: string;
};
export type BoardUpdateCheck = {status: 'current' | 'available' | 'source-older'; installedVersion?: string; availableVersion: string; message: string};
export type BoardUpdateResult = {changed: boolean; previousVersion: string; installedVersion: string; message: string; connection: Connection};
export type BoardInstallResult = {success: boolean; message: string; connection: Connection};
export type StudioUpdateCheck = {status: 'current' | 'ahead' | 'available' | 'unsupported'; installedVersion: string; availableVersion?: string; message: string; releaseUrl?: string};

export type BPUTensorDesc = {
  index: number;
  name: string;
  inputSource?: string;
  validShape: string;
  alignedShape: string;
  alignedBytes: number;
  tensorType: string;
  tensorLayout: string;
  quantiType: string;
  stride?: string;
  quantizeAxis?: number;
};

export type BPUModelInfo = {
  modelName: string;
  modelFile: string;
  targetSoc?: string;
  bpuPlatformVersion?: string;
  hbrtVersion?: string;
  dnnVersion?: string;
  modelBuilderVersion?: string;
  loadDdrCostMs: number;
  inputs: BPUTensorDesc[];
  outputs: BPUTensorDesc[];
  rawOutput?: string;
};

export type BPUBenchmarkRequest = {
  modelPath: string;
  modelName?: string;
  coreId: number;
  frameCount: number;
  threadCount: number;
  inputFile?: string;
};

export type BPUBenchmarkResult = {
  modelPath: string;
  modelName: string;
  coreId: number;
  threadCount: number;
  frameCount: number;
  fps: number;
  averageLatencyMs: number;
  minLatencyMs: number;
  maxLatencyMs: number;
  programRunTimeMs: number;
  totalLatencyMs: number;
  capturedAt: string;
  rawOutput?: string;
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

export type SupportFinding = {code: string; severity: 'info' | 'warning' | 'error'; scope: string; title: string; summary: string; action: string; count?: number};
export type SupportBundle = {schemaVersion?: number; id: string; createdAt: string; path: string; sizeBytes: number; sha256: string; excluded: string[]; status?: 'healthy' | 'attention' | 'action-required'; checks: {pass: number; info?: number; warn: number; fail: number}; findings?: SupportFinding[]};
export type DiagnosticCheck = {name: string; status: 'pass' | 'info' | 'warn' | 'fail'; summary: string};
export type DiagnosticRepairAction = {id: 'private-runtime-permissions' | 'restart-daemon'; executor: 'agentd' | 'client'; status: 'available' | 'blocked'; requiresConfirmation: true; summary: string; reason: string};
export type DiagnosticReport = {schemaVersion: 1; capturedAt: string; status: 'healthy' | 'attention' | 'action-required'; summary: {pass: number; info: number; warn: number; fail: number}; checks: DiagnosticCheck[]; findings: SupportFinding[]; repairs: DiagnosticRepairAction[]};

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
  state?: 'reviewing' | 'approved' | 'denied' | 'manual-required';
  decisionSource?: 'approval-model' | 'board-reviewer' | 'human';
  decisionReason?: string;
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
  approvalModel?: string;
  permissionMode?: 'review' | 'ask' | 'auto-review' | 'developer';
  sandboxMode?: 'review' | 'workspace' | 'system' | 'off';
  networkMode?: 'shared' | 'model-only' | 'offline';
  sandbox?: {requested: string; effective: string; backend: string; filesystemRestricted: boolean; devicesRestricted: boolean; capabilitiesDropped: boolean; networkRestricted: boolean; reason?: string};
  parentTaskId?: string;
  sourceTaskId?: string;
  forkSequence?: number;
  branchKind?: 'side' | 'edit';
  currentActivity?: string;
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

export type Schedule = {
  id: string;
  name: string;
  taskId: string;
  prompt?: string;
  at?: string;
  every?: string;
  enabled: boolean;
  status: 'active' | 'paused' | 'completed' | 'failed';
  createdAt: string;
  updatedAt: string;
  nextRun?: string;
  lastRun?: string;
  runCount: number;
  lastResult?: string;
  pending?: boolean;
  inFlight?: boolean;
  dispatchState?: string;
};

export type CreateScheduleRequest = {name: string; taskId: string; prompt: string; at?: string; every?: string};
export type EventPage = {
  events: TaskEvent[];
  nextAfter?: number;
  hasMore: boolean;
	  nextBefore?: number;
	  hasEarlier?: boolean;
  retainedFrom?: number;
  retainedThrough?: number;
  latestSequence?: number;
  historyTruncated?: boolean;
  cursorExpired?: boolean;
};
export type EventEnvelope = { boardId: string; event: TaskEvent };
export type ModelOption = {
  provider: string;
  id: string;
  name: string;
  default?: boolean;
  capabilities?: {reasoning: boolean; imageInput: boolean};
  capabilitySource?: 'runtime-model-table' | 'conservative-default' | string;
  managed?: boolean;
  modelOnly?: boolean;
};
export type ManagedProviderModel = {id: string; name?: string; contextWindow: number; maxTokens: number; reasoning: boolean; image: boolean};
export type ManagedProvider = {id: string; name?: string; api: 'anthropic-messages' | 'openai-completions' | 'openai-responses' | 'google-generative-ai'; models: ManagedProviderModel[]; credential: 'ready' | 'missing'; credentialUsers: number};
export type AddManagedProviderRequest = {id: string; name?: string; baseUrl: string; api: ManagedProvider['api']; model: string; modelName?: string; contextWindow?: number; maxTokens?: number; reasoning?: boolean; image?: boolean; authHeader?: boolean};
export type ProviderMutationResult = {saved: boolean; applied: boolean; message: string};
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
  schemaVersion?: number;
  scope?: 'gateway-protocol' | string;
  agentRuntimeStatus?: 'not-tested' | string;
  rdkTaskStatus?: 'not-tested' | string;
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
export type ModelRuntimeProbeCheck = {name: string; status: 'passed' | 'failed' | 'skipped' | string; message: string};
export type ModelRuntimeProbe = {
  schemaVersion: number;
  scope: string;
  provider: string;
  model: string;
  status: 'partial' | 'failed' | string;
  category?: string;
  message: string;
  reasoningDeclared: boolean;
  imageInputDeclared: boolean;
  checkedAt: string;
  durationMs?: number;
  checks: ModelRuntimeProbeCheck[];
  pending: string[];
};
export type ModelRDKProbeCheck = {name: string; status: 'passed' | 'failed' | 'skipped' | string; message: string};
export type ModelRDKProbeBinding = {
  productVersion: string;
  buildStatus: string;
  commit?: string;
  dirty?: boolean;
  buildTarget?: string;
  agentdBinarySha256?: string;
  piVersion?: string;
  piCommit?: string;
  piCompatibilitySha256?: string;
  expertPromptSha256: string;
  rdkExtensionSha256: string;
  knowledgePackSha256: string;
  knowledgeVersion: string;
  knowledgeUpdatedAt: string;
  board: string;
  boardId: string;
  rdkOsVersion: string;
  architecture: string;
};
export type ModelRDKProbe = {
  schemaVersion: number;
  scope: string;
  profile: string;
  provider: string;
  model: string;
  status: 'passed' | 'failed' | string;
  releaseEligible: boolean;
  category?: string;
  message: string;
  checkedAt: string;
  durationMs?: number;
  binding: ModelRDKProbeBinding;
  sources?: string[];
  checks: ModelRDKProbeCheck[];
  notCovered: string[];
};
export type ModelRDKProfileStatus = {
  id: string;
  name: string;
  workflow: string;
  evidenceClass: string;
  description: string;
  availability: 'available' | 'planned' | 'unsupported-target';
  evidenceState: 'untested' | 'current' | 'stale';
  targets: string[];
  notCovered: string[];
  staleReasons: ModelQualification['staleReasons'];
  result?: ModelRDKProbe;
};
export type ModelRDKMatrix = {
  schemaVersion: 1;
  provider: string;
  model: string;
  boardId: string;
  rdkOsVersion: string;
  architecture: string;
  capturedAt: string;
  profiles: ModelRDKProfileStatus[];
};
export type ModelQualification = {
  schemaVersion: 1;
  provider: string;
  model: string;
  state: 'untested' | 'current' | 'expired' | 'stale';
  level: 'untested' | 'route' | 'protocol' | 'runtime' | 'rdk-profile' | 'rdk-profile-release';
  outcome: 'unknown' | 'passed' | 'partial' | 'failed';
  updatedAt?: string;
  staleReasons: Array<'configuration-changed' | 'product-version-changed' | 'build-changed' | 'pi-runtime-changed' | 'board-changed' | 'rdk-resources-changed'>;
  staleLayers: Array<'route' | 'protocol' | 'runtime' | 'rdk'>;
  expiredLayers: Array<'route' | 'protocol' | 'runtime' | 'rdk'>;
  health?: ModelHealth;
  conformance?: ModelConformance;
  runtime?: ModelRuntimeProbe;
  rdk?: ModelRDKProbe;
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
export type ForkTaskRequest = {taskId: string; sequence?: number; prompt?: string; images?: ImageContent[]; name?: string; kind: 'side' | 'edit'; model?: string; approvalModel?: string; permissionMode?: string; sandboxMode?: 'review' | 'workspace' | 'system' | 'off'; networkMode?: 'shared' | 'model-only' | 'offline'};
