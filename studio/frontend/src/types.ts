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
};

export type DaemonInfo = {
  version: string;
  pid: number;
  startedAt: string;
  activeTasks: number;
  maximumTasks: number;
  stateRoot: string;
};

export type Connection = {
  board: Board;
  connected: boolean;
  reconnected?: boolean;
  daemon?: DaemonInfo;
  capabilities?: Capabilities;
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
export type StartDeploymentRequest = {cwd: string; artifactPath: string; goal: 'deploy-and-validate' | 'benchmark'; name?: string; model?: string; permissionMode?: string; profile?: string};

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

export type Task = {
  id: string;
  name: string;
  cwd: string;
  status: string;
  pid?: number;
  createdAt: string;
  updatedAt: string;
  lastSequence: number;
  lastError?: string;
  logTruncated?: boolean;
  sessionFile?: string;
  sessionId?: string;
  approved?: boolean;
  resumeCount?: number;
  restartCount?: number;
  model?: string;
  permissionMode?: 'review' | 'ask' | 'developer';
  parentTaskId?: string;
  forkSequence?: number;
  branchKind?: 'side' | 'edit';
  archivedAt?: string;
  pendingApprovals?: Approval[];
  deployment?: DeploymentRecord;
};

export type NormalizedEvent = {
  schema: number;
  type: string;
  data?: Record<string, unknown>;
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
export type ModelOption = {provider: string; id: string; name: string};
export type ImageContent = {type: 'image'; data: string; mimeType: string; name?: string};
export type AttachmentSummary = {name?: string; mimeType: string};
export type WorkspaceEntry = {name: string; path: string};
export type WorkspaceListing = {path: string; parent?: string; home: string; directories: WorkspaceEntry[]};
export type ForkTaskRequest = {taskId: string; sequence?: number; prompt: string; images?: ImageContent[]; name?: string; kind: 'side' | 'edit'; model?: string; permissionMode?: string};
