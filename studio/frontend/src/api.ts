import type {AddManagedProviderRequest, Board, BoardInstallResult, BoardUpdateCheck, BoardUpdateResult, BPUBenchmarkRequest, BPUBenchmarkResult, BPUModelInfo, Connection, CreateScheduleRequest, DeploymentInspection, DeploymentStatus, DiagnosticReport, EventEnvelope, EventPage, ExtensionCatalog, ForkTaskRequest, ImageContent, ManagedProvider, ModelConformance, ModelHealth, ModelOption, ModelQualification, ModelRDKMatrix, ModelRDKProbe, ModelRuntimeProbe, ProviderMutationResult, Schedule, StartDeploymentRequest, StudioUpdateCheck, SupportBundle, SystemSnapshot, Task, TaskPage, WorkspaceApplyResult, WorkspaceChanges, WorkspaceDelivery, WorkspaceIsolation, WorkspaceListing} from './types';
import {historyPageBefore} from './conversation-history.js';


export type TaskWatchStatus = {
  boardId: string;
  taskId: string;
  state: 'connected' | 'reconnecting' | 'failed';
  attempt?: number;
  message?: string;
  retainedFrom?: number;
  retainedThrough?: number;
  latestSequence?: number;
  historyTruncated?: boolean;
  cursorExpired?: boolean;
};

type Backend = Record<string, (...args: any[]) => Promise<any>>;

declare global {
  interface Window {
    go?: { main?: { App?: Backend } };
    runtime?: {
      EventsOn?: (name: string, callback: (...args: any[]) => void) => () => void;
      WindowToggleMaximise?: () => void;
      WindowSetSystemDefaultTheme?: () => void;
      WindowSetLightTheme?: () => void;
      WindowSetDarkTheme?: () => void;
    };
  }
}

const mockBoard: Board = {id: 's600-demo', name: 'RDK S600', host: 'rdk-s600.local', user: 'root', port: 22};
const now = new Date();
const mockTasks: Task[] = [
  {id: 'b8930da1a77e4a8f12345678', name: 'model-benchmark', cwd: '/root/yolo_bench_s100', status: 'idle', pid: 842107, createdAt: new Date(now.getTime() - 42 * 60_000).toISOString(), updatedAt: now.toISOString(), lastSequence: 2400, sessionId: '019fef9b-695f-7e9d', sessionFile: '/root/.local/state/hobot-code/sessions/demo.jsonl', model: 'drobotics/kimi-k3'},
  {id: '04acf83b820b934e12345678', name: 'camera-pipeline', cwd: '/root/tros_ws', status: 'waiting', pid: 842244, createdAt: new Date(now.getTime() - 18 * 60_000).toISOString(), updatedAt: now.toISOString(), lastSequence: 72, pendingApprovals: [{id: 'approval-demo', method: 'select', title: 'Allow bash?\nRisk: Executes a shell command with the current user privileges.\nTarget: hobot-board info', message: 'Choose how Hobot Code may run this tool.', options: ['Allow once', 'Allow network for this task', 'Deny'], active: true}]},
  {id: 'f30bb47e8d552f1812345678', name: 'deploy-review', cwd: '/root/yolo_bench_s100', status: 'idle', pid: 841992, createdAt: new Date(now.getTime() - 70 * 60_000).toISOString(), updatedAt: now.toISOString(), lastSequence: 53, parentTaskId: 'b8930da1a77e4a8f12345678', branchKind: 'side', model: 'drobotics/kimi-k3'},
];

const mockPrompts = [
  '检查 S100 的运行时状态，并列出需要确认的指标。',
  '先读取部署日志，不要修改板端文件。',
  '把延迟结果按 p50 和 p95 汇总。',
  '这个代码块的参数是否安全？\n```yaml\nbatch: 4\nprecision: fp16\n```',
  '比较这次结果和上一轮基线。',
  '确认摄像头链路是否已经稳定。',
  '只给出可以复现的下一步。',
  '检查工作区变更，再继续验证。',
  '总结当前风险，不要启动新的任务。',
  '现在生成最终的验收结论。',
];

const mockHistoryEvents = (taskId: string) => Array.from({length: 2400}, (_, index) => {
  const sequence = index + 1;
  const turn = Math.floor(index / 24);
  const time = new Date(now.getTime() - (2400 - sequence) * 4_000).toISOString();
  if (sequence % 24 === 1) return {protocol: 1, kind: 'event', taskId, sequence, time, event: {type: 'hobot_user_prompt'}, normalized: {schema: 4, type: 'user.message', data: {text: `${mockPrompts[turn % mockPrompts.length]} (${turn + 1})`}}};
  if (sequence % 24 === 0) return {protocol: 1, kind: 'event', taskId, sequence, time, event: {type: 'task_idle'}, normalized: {schema: 4, type: 'task.idle'}};
  return {protocol: 1, kind: 'event', taskId, sequence, time, event: {type: 'message_update'}, normalized: {schema: 4, type: 'assistant.text.delta', data: {delta: `Checked evidence item ${sequence}. `}}};
});

export function mockHistoryPage(taskId: string, before = 0, limit = 200): EventPage {
  return historyPageBefore(mockHistoryEvents(taskId), before, limit);
}

const mockBackend: Backend = {
  GetAppVersion: async () => '0.28.0',
	CheckStudioUpdate: async (): Promise<StudioUpdateCheck> => ({status: 'current', installedVersion: '0.28.0', availableVersion: '0.28.0', message: 'Hobot Code Studio is current.', releaseUrl: 'https://github.com/bryant-w/hobot-code/releases/tag/v0.28.0'}),
  OpenStudioUpdate: async () => undefined,
  ListBoards: async () => [mockBoard],
  ProbeBoard: async (board: Board): Promise<Connection> => ({board, connected: true, daemon: {version: '0.28.0', pid: 834124, startedAt: now.toISOString(), activeTasks: 3, maximumTasks: 3, stateRoot: '/root/.local/state/hobot-code'}, capabilities: {protocolMin: 1, protocolMax: 1, eventSchema: 4, capabilities: ['extensions.catalog.v1', 'schedules.v1', 'events.normalized.v3', 'events.normalized.v4', 'events.items.v1', 'events.retention.v1', 'events.page.before.v1', 'tasks.queue.v1', 'tasks.failure.v1', 'workspaces.changes.v1', 'workspaces.isolation.v1', 'workspaces.write-leases.v1', 'system.snapshot', 'tasks.lifecycle', 'tasks.page', 'events.page'], maximumActiveTasks: 3, maximumRetainedTasks: 100}, snapshot: {capturedAt: now.toISOString(), board: 'D-Robotics RDK S600', boardId: 's600', hostname: 'rdk', rdkOsVersion: '5.1.0', kernel: '6.1.158-rt58', architecture: 'arm64', cpuCores: 18, loadAverage: [], memory: {totalBytes: 0, availableBytes: 0}, disk: {path: '/root', totalBytes: 0, availableBytes: 0}, thermalZones: [], bpuDevices: [], rdkUtilities: {}, uptimeSeconds: 0}, compatibility: {status: 'supported', summary: 'Board and Studio capabilities are compatible.', appVersion: '0.28.0', agentdVersion: '0.28.0', protocol: 1, eventSchema: 4, boardId: 's600', rdkOsVersion: '5.1.0', validatedTarget: true, issues: []}}),
  SaveBoard: async (board: Board) => ({...board, id: board.id || `board-${Date.now()}`}),
  RemoveBoard: async () => undefined,
  ConnectBoard: async (id: string): Promise<Connection> => ({board: {...mockBoard, id}, connected: true, daemon: {version: '0.28.0', pid: 834124, startedAt: now.toISOString(), activeTasks: 3, maximumTasks: 3, maximumSideTasks: 2, stateRoot: '/root/.local/state/hobot-code'}, capabilities: {protocolMin: 1, protocolMax: 1, eventSchema: 4, capabilities: ['extensions.catalog.v1', 'providers.manage.v1', 'events.normalized.v3', 'events.normalized.v4', 'events.items.v1', 'events.retention.v1', 'tasks.queue.v1', 'tasks.failure.v1', 'workspaces.changes.v1', 'workspaces.isolation.v1', 'workspaces.write-leases.v1', 'workspaces.delivery.v1', 'system.snapshot', 'diagnostics.inspect.v1', 'diagnostics.repair.v1', 'support.bundle.v1', 'support.bundle.v2', 'deployments.v1', 'tasks.lifecycle', 'tasks.page', 'events.page', 'tasks.resume', 'tasks.restart', 'tasks.fork', 'tasks.fork.deferred-prompt.v1', 'tasks.collaboration.v1', 'tasks.models', 'tasks.permissions', 'tasks.sandbox.v1', 'tasks.network.v1', 'tasks.images', 'models.capabilities.v1', 'models.health.v1', 'models.conformance.v1', 'models.runtime-probe.v1', 'models.rdk-probe.v1', 'models.rdk-matrix.v1', 'models.qualification.v1', 'workspaces.browse', 'bridge.stdio'], maximumActiveTasks: 3, maximumSideTasks: 2, maximumRetainedTasks: 100, sandbox: {available: true, backend: 'bubblewrap', profiles: ['review', 'workspace', 'system', 'off'], networkModes: ['shared', 'offline'], filesystemWritesRestricted: true, devicesRestricted: true, capabilitiesDropped: true, networkRestricted: true}}, compatibility: {status: 'supported', summary: 'Board and Studio capabilities are compatible.', appVersion: '0.28.0', agentdVersion: '0.28.0', protocol: 1, eventSchema: 4, boardId: 's600', rdkOsVersion: '5.1.0', validatedTarget: true, issues: []}}),
  RefreshBoard: async (id: string): Promise<Connection> => ({board: {...mockBoard, id}, connected: true, daemon: {version: '0.25.0', pid: 834124, startedAt: now.toISOString(), activeTasks: 3, maximumTasks: 3, stateRoot: '/root/.local/state/hobot-code'}, capabilities: {protocolMin: 1, protocolMax: 1, eventSchema: 3, capabilities: ['events.normalized.v3', 'system.snapshot', 'tasks.lifecycle', 'tasks.page', 'events.page'], maximumActiveTasks: 3, maximumRetainedTasks: 100}, compatibility: {status: 'limited', summary: 'Connected with limited features.', appVersion: '0.28.0', agentdVersion: '0.25.0', protocol: 1, eventSchema: 3, boardId: 's600', rdkOsVersion: '5.1.0', validatedTarget: true, issues: [{code: 'version-line-mismatch', severity: 'warning', message: 'Studio and board service are from different release lines.', action: 'Update Hobot Code on the board.'}]}}),
  GetSystemSnapshot: async (): Promise<SystemSnapshot> => ({capturedAt: new Date().toISOString(), board: 'D-Robotics RDK S600', boardId: 's600', hostname: 'drobot', rdkOsVersion: '5.1.0', kernel: '6.1.158-rt58', architecture: 'arm64', cpuCores: 18, loadAverage: [6.18, 5.72, 5.4], memory: {totalBytes: 11 * 1024 ** 3, availableBytes: 4.4 * 1024 ** 3}, disk: {path: '/root/.local/state/hobot-code', totalBytes: 220 * 1024 ** 3, availableBytes: 188 * 1024 ** 3}, thermalZones: [{name: 'pvt_cmn', celsius: 44.2}, {name: 'pvt_bpu0', celsius: 43.1}, {name: 'pvt_bpu1', celsius: 43.8}], bpuDevices: ['/dev/bpu', '/dev/bpu_core0', '/dev/bpu_core1', '/dev/bpu_core2', '/dev/bpu_core3'], bpuCores: [{index: 0, name: 'BPU 0', utilizationPercent: 68, currentFrequencyHz: 1_500_000_000, maximumFrequencyHz: 1_500_000_000}, {index: 1, name: 'BPU 1', utilizationPercent: 42, currentFrequencyHz: 1_500_000_000, maximumFrequencyHz: 1_500_000_000}, {index: 2, name: 'BPU 2', utilizationPercent: 17, currentFrequencyHz: 1_333_330_000, maximumFrequencyHz: 1_500_000_000}, {index: 3, name: 'BPU 3', utilizationPercent: 5, currentFrequencyHz: 1_333_330_000, maximumFrequencyHz: 1_500_000_000}], aiMemory: {available: true, bpuAllocationAvailable: true, ionAvailable: true, cmaAvailable: false, dmaBufAvailable: true, bpuAllocatedBytes: 384 * 1024 ** 2, ionAllocatedBytes: 712 * 1024 ** 2, ionOrphanedBytes: 0, dmaBufBytes: 96 * 1024 ** 2, dmaBufObjects: 18}, accelerator: {available: true, source: 'ion-debugfs', capturedAt: new Date().toISOString(), ddrReadMiBps: 324, ddrWriteMiBps: 118, hbmemPools: [{name: 'cma_reserved', totalBytes: 1024 ** 3, usedBytes: 384 * 1024 ** 2, freeBytes: 640 * 1024 ** 2, processBytes: 96 * 1024 ** 2, systemBytes: 288 * 1024 ** 2}, {name: 'ion_cma', totalBytes: 512 * 1024 ** 2, usedBytes: 96 * 1024 ** 2, freeBytes: 416 * 1024 ** 2, processBytes: 32 * 1024 ** 2, systemBytes: 64 * 1024 ** 2}, {name: 'carveout', totalBytes: 512 * 1024 ** 2, usedBytes: 242 * 1024 ** 2, freeBytes: 270 * 1024 ** 2, processBytes: 200 * 1024 ** 2, systemBytes: 42 * 1024 ** 2}], processes: [{pid: 18422, name: 'hrt_model_exec', rssBytes: 512 * 1024 ** 2, hbmemBytes: 328 * 1024 ** 2}]}, rdkUtilities: {hrut_somstatus: true, hrt_model_exec: true, rdkos_info: true}, uptimeSeconds: 186420}),
  CheckBoardUpdate: async (): Promise<BoardUpdateCheck> => ({status: 'available', installedVersion: '0.26.0', availableVersion: '0.28.0', message: 'A verified stable update is available.'}),
	InstallBoardUpdate: async (id: string): Promise<BoardUpdateResult> => ({changed: true, previousVersion: '0.26.0', installedVersion: '0.28.0', message: 'The board update was verified and Studio reconnected.', connection: await mockBackend.ConnectBoard(id)}),
  InstallBoardService: async (board: Board): Promise<BoardInstallResult> => ({success: true, message: 'Hobot Code was successfully installed and connected.', connection: await mockBackend.ProbeBoard(board)}),
  SaveSupportBundle: async (): Promise<SupportBundle> => ({schemaVersion: 2, id: 'a1b2c3d4e5f6', createdAt: new Date().toISOString(), path: '/Users/demo/Downloads/hobot-code-support-a1b2c3d4e5f6.json', sizeBytes: 4096, sha256: '8f1b195d2bafc181677b28ce0d1a8d032417e488703564c4a0a4ed10d6f103bc', excluded: ['prompts'], status: 'attention', checks: {pass: 8, info: 2, warn: 1, fail: 0}, findings: [{code: 'memory', severity: 'warning', scope: 'resources', title: 'System memory is low', summary: '18.0% available', action: 'Stop unused tasks or services before starting another model, conversion, or build workload.'}]}),

  GetDiagnostics: async (): Promise<DiagnosticReport> => ({schemaVersion: 1, capturedAt: new Date().toISOString(), status: 'attention', summary: {pass: 12, info: 2, warn: 1, fail: 0}, checks: [{name: 'model-configuration', status: 'pass', summary: 'private provider credentials are available'}, {name: 'release-integrity', status: 'pass', summary: 'release is verified'}, {name: 'memory', status: 'warn', summary: '18.0% available'}], findings: [{code: 'memory', severity: 'warning', scope: 'resources', title: 'System memory is low', summary: '18.0% available', action: 'Stop unused workloads before starting a model conversion.'}], repairs: []}),
  RepairDiagnostics: async (_id: string, _action: string, _confirmed: boolean): Promise<DiagnosticReport> => mockBackend.GetDiagnostics(),
  InspectDeployment: async (_board: string, cwd: string): Promise<DeploymentInspection> => ({capturedAt: new Date().toISOString(), cwd, board: 'D-Robotics RDK S600', boardId: 's600', rdkOsVersion: '5.1.0', truncated: false, artifacts: [{path: `${cwd}/model.onnx`, relativePath: 'model.onnx', name: 'model.onnx', kind: 'onnx', sizeBytes: 48 * 1024 ** 2, modifiedAt: new Date().toISOString(), compatibility: 'conversion-required', reason: 'source model requires board-specific conversion and quantization'}, {path: `${cwd}/output/detector_s600.hbm`, relativePath: 'output/detector_s600.hbm', name: 'detector_s600.hbm', kind: 'rdk-hbm', sizeBytes: 23 * 1024 ** 2, modifiedAt: new Date().toISOString(), compatibility: 'candidate', reason: 'filename matches the current board; runtime validation is still required'}]}),
  StartDeployment: async (_board: string, request: StartDeploymentRequest): Promise<Task> => ({...mockTasks[0], id: `task-${Date.now()}`, name: request.name || `Deploy ${request.artifactPath.split('/').at(-1)}`, cwd: request.cwd, status: 'starting'}),
  GetDeploymentStatus: async (_board: string, taskId: string): Promise<DeploymentStatus> => ({taskId, phase: 'running', deployment: {schema: 1, cwd: '/root/yolo_bench_s100', board: 'D-Robotics RDK S600', boardId: 's600', rdkOsVersion: '5.1.0', goal: 'deploy-and-validate', artifact: {path: '/root/yolo_bench_s100/model.onnx', relativePath: 'model.onnx', name: 'model.onnx', kind: 'onnx', sizeBytes: 1, modifiedAt: new Date().toISOString(), compatibility: 'conversion-required', reason: 'conversion required'}, reportPath: '/root/yolo_bench_s100/.hobot/deployments/demo.json', createdAt: new Date().toISOString()}}),
  InspectBPUModel: async (_board: string, modelPath: string): Promise<BPUModelInfo> => ({
    modelName: 'yolov8n_640x640_nv12',
    modelFile: modelPath,
    targetSoc: 'x5',
    bpuPlatformVersion: '1.3.6',
    hbrtVersion: '3.15.55.0',
    dnnVersion: '1.24.5',
    modelBuilderVersion: '1.23.6',
    loadDdrCostMs: 379.39,
    inputs: [
      {index: 0, name: 'images', inputSource: 'HB_DNN_INPUT_FROM_PYRAMID', validShape: '(1,3,640,640)', alignedShape: '(1,3,640,640)', alignedBytes: 614400, tensorType: 'HB_DNN_IMG_TYPE_NV12', tensorLayout: 'HB_DNN_LAYOUT_NCHW', quantiType: 'NONE'}
    ],
    outputs: [
      {index: 0, name: 'output0', validShape: '(1,80,80,80)', alignedShape: '(1,80,80,80)', alignedBytes: 2048000, tensorType: 'HB_DNN_TENSOR_TYPE_F32', tensorLayout: 'HB_DNN_LAYOUT_NHWC', quantiType: 'NONE'},
      {index: 1, name: '318', validShape: '(1,80,80,64)', alignedShape: '(1,80,80,64)', alignedBytes: 1638400, tensorType: 'HB_DNN_TENSOR_TYPE_S32', tensorLayout: 'HB_DNN_LAYOUT_NHWC', quantiType: 'SCALE', quantizeAxis: 3}
    ]
  }),
  RunBPUBenchmark: async (_board: string, req: BPUBenchmarkRequest): Promise<BPUBenchmarkResult> => ({
    modelPath: req.modelPath,
    modelName: 'yolov8n_640x640_nv12',
    coreId: req.coreId,
    threadCount: req.threadCount || 1,
    frameCount: req.frameCount || 200,
    fps: 80.26 * (req.threadCount > 1 ? req.threadCount * 0.95 : 1),
    averageLatencyMs: 12.42,
    minLatencyMs: 6.25,
    maxLatencyMs: 13.36,
    programRunTimeMs: 2491.78,
    totalLatencyMs: 2485.71,
    capturedAt: new Date().toISOString()
  }),
  ListWorkspaceBPUModels: async (_board: string, cwd: string): Promise<string[]> => [
    `${cwd}/yolov8_640x640_nv12.bin`,
    `${cwd}/mobileone_s0_224_rgb_x5.bin`
  ],
  DownloadSampleBPUModel: async (_board: string, _soc: string): Promise<string> => '/root/models/mobilenetv2_224x224_nv12.bin',
  UploadBPUModel: async (_board: string, filename: string, _data: string): Promise<string> => `/root/models/${filename}`,
  DeleteBPUModel: async (_board: string, _modelPath: string): Promise<void> => undefined,



  DisconnectBoard: async () => undefined,
  RefreshTasks: async (): Promise<TaskPage> => ({tasks: mockTasks}),
  GetTask: async (_board: string, taskId: string) => mockTasks.find((task) => task.id === taskId),
  ListSchedules: async (): Promise<Schedule[]> => [],
  CreateSchedule: async (_board: string, request: CreateScheduleRequest): Promise<Schedule> => ({id: `schedule-${Date.now()}`, name: request.name, taskId: request.taskId, every: request.every, at: request.at, enabled: true, status: 'active', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(), nextRun: request.at || new Date(Date.now() + 60_000).toISOString(), runCount: 0}),
  PauseSchedule: async (_board: string, id: string): Promise<Schedule> => ({id, name: 'Schedule', taskId: mockTasks[0].id, enabled: false, status: 'paused', createdAt: now.toISOString(), updatedAt: new Date().toISOString(), runCount: 0}),
  ResumeSchedule: async (_board: string, id: string): Promise<Schedule> => ({id, name: 'Schedule', taskId: mockTasks[0].id, enabled: true, status: 'active', createdAt: now.toISOString(), updatedAt: new Date().toISOString(), runCount: 0}),
  RunSchedule: async (_board: string, id: string): Promise<Schedule> => ({id, name: 'Schedule', taskId: mockTasks[0].id, enabled: true, status: 'active', createdAt: now.toISOString(), updatedAt: new Date().toISOString(), runCount: 1, pending: true}),
  DeleteSchedule: async () => undefined,
  GetEvents: async (_board: string, taskId: string, after = 0, limit = 200) => {
		const all = mockHistoryEvents(taskId).filter((event) => event.sequence > after);
		const events = all.slice(0, Math.max(1, Math.min(200, limit)));
		return {...mockHistoryPage(taskId), events, nextAfter: events.at(-1)?.sequence, hasMore: all.length > events.length};
	},
	GetEventsBefore: async (_board: string, taskId: string, before = 0, limit = 200) => mockHistoryPage(taskId, before, limit),
  StartTask: async (_board: string, request: any) => ({...mockTasks[2], id: `task-${Date.now()}`, name: request.name || 'new-task', cwd: request.cwd, projectCwd: request.cwd, workspaceMode: request.workspaceMode || 'shared', status: 'starting'}),
  SendPrompt: async () => undefined,
  AbortTask: async () => undefined,
  StopTask: async () => undefined,
  DeleteTasks: async () => undefined,
  ResumeTask: async (_board: string, taskId: string) => ({...mockTasks.find((task) => task.id === taskId), status: 'starting'}),
  RestartTask: async (_board: string, taskId: string) => ({...mockTasks.find((task) => task.id === taskId), status: 'starting', sessionFile: undefined, sessionId: undefined}),
  SetTaskModel: async () => undefined,
  SetTaskPermissionMode: async (_board: string, taskId: string, mode: string) => ({...mockTasks.find((task) => task.id === taskId), permissionMode: mode}),
  SetTaskApprovalModel: async (_board: string, taskId: string, model: string) => ({...mockTasks.find((task) => task.id === taskId), approvalModel: model}),
  SetTaskSandboxMode: async (_board: string, taskId: string, mode: string) => ({...mockTasks.find((task) => task.id === taskId), sandboxMode: mode}),
  SetTaskNetworkMode: async (_board: string, taskId: string, mode: string) => ({...mockTasks.find((task) => task.id === taskId), networkMode: mode}),
  RenameTask: async (_board: string, taskId: string, name: string) => ({...mockTasks.find((task) => task.id === taskId), name}),
  ListModels: async (): Promise<ModelOption[]> => [
    {provider: 'drobotics', id: 'kimi-k3', name: 'kimi-k3', default: true, capabilities: {reasoning: true, imageInput: true}, capabilitySource: 'runtime-model-table'},
    {provider: 'drobotics', id: 'qwen3.8-max', name: 'qwen3.8-max', capabilities: {reasoning: true, imageInput: true}, capabilitySource: 'runtime-model-table'},
    {provider: 'drobotics', id: 'glm-5.2', name: 'glm-5.2', capabilities: {reasoning: true, imageInput: true}, capabilitySource: 'runtime-model-table'},
    {provider: 'drobotics', id: 'deepseek/deepseek-v4-flash', name: 'deepseek/deepseek-v4-flash', capabilities: {reasoning: true, imageInput: false}, capabilitySource: 'runtime-model-table'},
    {provider: 'drobotics', id: 'deepseek-v4-pro', name: 'deepseek-v4-pro', capabilities: {reasoning: true, imageInput: false}, capabilitySource: 'runtime-model-table'},
	{provider: 'acme', id: 'coder-v2', name: 'Acme Coder', managed: true, capabilities: {reasoning: true, imageInput: true}, capabilitySource: 'runtime-model-table'},
  ],
  ListManagedProviders: async (): Promise<ManagedProvider[]> => [{id: 'acme', name: 'Acme Gateway', api: 'openai-responses', credential: 'ready', credentialUsers: 1, models: [{id: 'coder-v2', name: 'Acme Coder', contextWindow: 65536, maxTokens: 4096, reasoning: true, image: true}]}],
  AddManagedProvider: async (): Promise<ProviderMutationResult> => ({saved: true, applied: false, message: 'Provider saved; active Agent work prevented a safe restart. Apply after tasks are idle.'}),
  RotateManagedProviderCredential: async (): Promise<ProviderMutationResult> => ({saved: true, applied: true, message: 'Provider key rotated and applied.'}),
  RemoveManagedProvider: async (): Promise<ProviderMutationResult> => ({saved: true, applied: true, message: 'Provider removed and applied.'}),
  ApplyProviderConfiguration: async (): Promise<ProviderMutationResult> => ({saved: true, applied: true, message: 'Provider configuration and applied.'}),
  ListExtensions: async (): Promise<ExtensionCatalog> => ({
    schemaVersion: 1, apiVersion: 'hobot.extensions/v1', productVersion: '0.28.0', hostVersion: '0.28.0', capturedAt: new Date().toISOString(),
    entries: [
      {id: 'hobot.rdk-core', name: 'RDK development core', version: '0.28.0', kind: 'extension', description: 'D-Robotics models, tools, knowledge, safety, and board coordination.', origin: 'built-in', scope: 'system', runtime: 'pi-extension', entrypoint: 'rdk/index.ts', trust: 'product', defaultEnabled: true, required: true, provides: ['provider.drobotics'], requires: ['pi.extension-api'], permissions: ['workspace', 'subprocess', 'rdk-devices'], targets: ['x5', 's100', 's600'], status: 'included'},
      {id: 'hobot.skill.board', name: 'RDK board development', version: '0.28.0', kind: 'skill', description: 'Board discovery, BPU workflows, deployment checks, and official RDK knowledge.', origin: 'built-in', scope: 'system', runtime: 'pi-skill', entrypoint: 'skills/rdk-board/SKILL.md', trust: 'product', defaultEnabled: true, required: false, provides: ['skill.rdk-board'], requires: ['hobot.rdk-core'], permissions: ['workspace', 'subprocess', 'rdk-devices'], targets: ['x5', 's100', 's600'], status: 'included'},
      {id: 'user.provider.lab-gateway', name: 'lab-gateway', version: 'configured', kind: 'provider', description: 'User-configured model provider with 3 models.', origin: 'user', scope: 'user', runtime: 'pi-provider', entrypoint: 'models.json#providers', trust: 'user', defaultEnabled: true, required: false, provides: ['provider.lab-gateway'], requires: [], permissions: ['model-network'], targets: [], status: 'configured'},
      {id: 'user.hook.quality-guard', name: 'quality-guard', version: 'configured', kind: 'integration', description: 'PreToolUse policy hook for bash.', origin: 'user', scope: 'user', runtime: 'hobot-hook', entrypoint: 'hooks.json#quality-guard', trust: 'user', defaultEnabled: true, required: false, provides: ['hook.pretooluse'], requires: [], permissions: ['subprocess'], targets: [], status: 'configured'},
      {id: 'user.lsp.clangd', name: 'clangd', version: 'configured', kind: 'integration', description: 'Language intelligence for C and C++ with 4 file types.', origin: 'user', scope: 'user', runtime: 'lsp-process', entrypoint: 'lsp.json#clangd', trust: 'user', defaultEnabled: true, required: false, provides: ['lsp.cpp'], requires: [], permissions: ['subprocess', 'workspace'], targets: [], status: 'missing'},
    ],
    diagnostics: [{source: 'providers', status: 'ok', message: 'Provider configuration inspected'}, {source: 'hooks', status: 'ok', message: 'Hook configuration inspected'}, {source: 'lsp', status: 'ok', message: 'LSP configuration inspected'}],
    policy: {inventoryOnly: true, executionAuthority: 'pi-runtime', permissionAuthority: 'board', thirdPartyRuntime: 'current-user', hotReload: false},
  }),
  CheckModelHealth: async (_board: string, model: string): Promise<ModelHealth> => ({provider: model.split('/')[0], model: model.split('/').slice(1).join('/'), status: 'available', category: 'ok', message: 'The model gateway completed a minimal response successfully.', transport: 'sse', checkedAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 300_000).toISOString(), firstByteMs: 326, latencyMs: 612, attempts: 1, cached: false}),
  VerifyModel: async (_board: string, model: string): Promise<ModelConformance> => ({schemaVersion: 1, scope: 'gateway-protocol', agentRuntimeStatus: 'not-tested', rdkTaskStatus: 'not-tested', provider: model.split('/')[0], model: model.split('/').slice(1).join('/'), status: 'verified', message: 'The gateway protocol probe passed. Agent runtime behavior and RDK task quality were not tested.', checkedAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 3_600_000).toISOString(), durationMs: 1420, attempts: 3, cached: false, checks: [{name: 'streaming', status: 'passed', category: 'ok', message: 'The gateway completed a bounded stream with an explicit terminal event.', latencyMs: 420}, {name: 'tool-call', status: 'passed', category: 'ok', message: 'The model emitted the requested tool call with valid structured arguments.', latencyMs: 420}, {name: 'tool-result', status: 'passed', category: 'ok', message: 'The model accepted the matching tool result and entered a valid next assistant turn.', latencyMs: 530}, {name: 'image-input', status: 'passed', category: 'ok', message: 'The gateway accepted a valid image input and completed the response.', latencyMs: 470}]}),
  ProbeModelRuntime: async (_board: string, model: string): Promise<ModelRuntimeProbe> => ({schemaVersion: 1, scope: 'agent-runtime-partial', provider: model.split('/')[0], model: model.split('/').slice(1).join('/'), status: 'partial', message: 'The isolated Agent runtime suite passed; broader RDK task coverage remains pending.', reasoningDeclared: true, imageInputDeclared: true, checkedAt: new Date().toISOString(), durationMs: 9200, checks: [{name: 'tool-call', status: 'passed', message: 'Structured tools completed.'}, {name: 'session-recovery', status: 'passed', message: 'Interrupted session recovered.'}], pending: ['rdk-task-suite']}),
  ProbeModelRDK: async (_board: string, model: string, profile = 'read-only-rdk-diagnostic-v1'): Promise<ModelRDKProbe> => ({schemaVersion: 1, scope: 'rdk-task-profile', profile, provider: model.split('/')[0], model: model.split('/').slice(1).join('/'), status: 'passed', releaseEligible: false, message: 'The selected read-only RDK profile passed with development-build evidence.', checkedAt: new Date().toISOString(), durationMs: 4800, binding: {productVersion: '0.28.0', buildStatus: 'development', dirty: true, buildTarget: 'linux-arm64', expertPromptSha256: 'a'.repeat(64), rdkExtensionSha256: 'b'.repeat(64), knowledgePackSha256: 'c'.repeat(64), knowledgeVersion: '2026.08', knowledgeUpdatedAt: '2026-08-14', board: 'D-Robotics RDK S600', boardId: 's600', rdkOsVersion: '5.1.0', architecture: 'arm64'}, sources: ['https://developer.d-robotics.cc/rdk_doc/'], checks: [{name: 'target-identity', status: 'passed', message: 'Board identity matched.'}, {name: 'versioned-knowledge', status: 'passed', message: 'Version-matched official knowledge was used.'}], notCovered: profile === 'read-only-model-deployment-planning-v1' ? ['model-conversion', 'board-inference', 'accuracy-validation', 'performance-benchmark'] : ['workspace-coding', 'model-deployment', 'multimedia-pipeline', 'hardware-control']}),
  GetModelRDKMatrix: async (_board: string, model: string): Promise<ModelRDKMatrix> => ({schemaVersion: 1, provider: model.split('/')[0], model: model.split('/').slice(1).join('/'), boardId: 's600', rdkOsVersion: '5.1.0', architecture: 'arm64', capturedAt: new Date().toISOString(), profiles: [
    {id: 'read-only-rdk-diagnostic-v1', name: 'Board diagnostics', workflow: 'board-diagnostics', evidenceClass: 'live-read-only', description: 'Live board identity and version-matched diagnostic knowledge.', availability: 'available', evidenceState: 'untested', targets: ['x5', 's100', 's600'], notCovered: ['workspace-coding', 'model-deployment', 'multimedia-pipeline', 'hardware-control'], staleReasons: []},
    {id: 'read-only-model-deployment-planning-v1', name: 'Model deployment planning', workflow: 'model-deployment', evidenceClass: 'knowledge-grounded-planning', description: 'Board-aware conversion, quantization, inference, and validation planning.', availability: 'available', evidenceState: 'untested', targets: ['x5', 's100', 's600'], notCovered: ['model-conversion', 'board-inference', 'accuracy-validation', 'performance-benchmark'], staleReasons: []},
    {id: 'read-only-multimedia-planning-v1', name: 'Multimedia pipeline planning', workflow: 'multimedia-pipeline', evidenceClass: 'knowledge-grounded-planning', description: 'Board-aware camera, codec, display, and TROS pipeline planning.', availability: 'available', evidenceState: 'untested', targets: ['x5', 's100', 's600'], notCovered: ['camera-capture', 'codec-execution', 'pipeline-throughput', 'device-integration'], staleReasons: []},
    {id: 'read-only-hardware-safety-planning-v1', name: 'Hardware safety planning', workflow: 'hardware-safety', evidenceClass: 'knowledge-grounded-planning', description: 'Board-aware power, boot, permission, rollback, and control-risk planning.', availability: 'available', evidenceState: 'untested', targets: ['x5', 's100', 's600'], notCovered: ['gpio-write', 'can-control', 'firmware-update', 'power-cycle'], staleReasons: []},
    {id: 'isolated-workspace-coding-v1', name: 'Workspace coding', workflow: 'workspace-coding', evidenceClass: 'not-implemented', description: 'Isolated repository inspection, bounded edit, verification, and change reporting.', availability: 'planned', evidenceState: 'untested', targets: ['x5', 's100', 's600'], notCovered: ['repository-edit', 'quality-gate', 'change-review'], staleReasons: []},
  ]}),
  GetModelQualification: async (_board: string, model: string): Promise<ModelQualification> => ({schemaVersion: 1, provider: model.split('/')[0], model: model.split('/').slice(1).join('/'), state: 'untested', level: 'untested', outcome: 'unknown', staleReasons: [], staleLayers: [], expiredLayers: []}),
  BrowseWorkspace: async (_board: string, path: string): Promise<WorkspaceListing> => ({path: path || '/root', parent: path === '/root' || !path ? '/' : '/root', home: '/root', directories: [{name: 'models', path: '/root/models'}, {name: 'tros_ws', path: '/root/tros_ws'}, {name: 'yolo_bench_s100', path: '/root/yolo_bench_s100'}]}),
  CreateWorkspace: async (_board: string, parent: string, name: string): Promise<WorkspaceListing> => ({path: `${parent}/${name}`.replace('//', '/'), parent, home: '/root', directories: []}),
  GetWorkspaceChanges: async (): Promise<WorkspaceChanges> => ({capturedAt: new Date().toISOString(), available: true, repository: true, repositoryRoot: '/root/yolo_bench_s100', scope: '.', head: '84f5a12c3d9e', files: [{path: 'README.md', status: '.M', kind: 'modified', unstaged: true}, {path: 'notes.txt', status: '??', kind: 'untracked', untracked: true}], patch: 'diff --git a/README.md b/README.md\nindex 7b45c01..fe1092a 100644\n--- a/README.md\n+++ b/README.md\n@@ -1,3 +1,4 @@\n # YOLO benchmark\n+Validated on RDK S600.\n'}),
  InspectWorkspaceIsolation: async (_board: string, path: string): Promise<WorkspaceIsolation> => ({capturedAt: new Date().toISOString(), available: true, repository: true, eligible: true, recommendedMode: 'worktree', repositoryRoot: path, scope: '.', head: '84f5a12c3d9ef90a', clean: true, reason: 'A clean Git repository can be isolated from other tasks.'}),
  InspectWorkspaceDelivery: async (_board: string, taskId: string): Promise<WorkspaceDelivery> => ({taskId, ready: true, reason: 'Changes can be applied to the original project as staged Git changes.', patchBytes: 384, digest: '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef'}),
  ApplyWorkspace: async (_board: string, taskId: string, expectedDigest: string): Promise<WorkspaceApplyResult> => ({taskId, applied: true, staged: true, patchBytes: 384, digest: expectedDigest, appliedAt: new Date().toISOString()}),
  ForkTask: async (_board: string, request: ForkTaskRequest) => ({...mockTasks[2], id: `task-${Date.now()}`, name: request.name || `${mockTasks[0].name}-side`, status: request.prompt ? 'starting' : 'stopped', awaitingPrompt: !request.prompt, parentTaskId: request.taskId, forkSequence: request.sequence, branchKind: request.kind}),
  RespondApproval: async () => undefined,
  WatchTask: async () => undefined,
  StopWatchingTask: async () => undefined,
  OpenExternalURL: async (url: string) => { window.open(url, '_blank', 'noopener,noreferrer'); },
};

const withMockHistoryCapability = async (value: Promise<Connection>): Promise<Connection> => {
  const connection = await value;
  const capabilities = connection.capabilities?.capabilities;
  if (capabilities && !capabilities.includes('events.page.before.v1')) capabilities.push('events.page.before.v1');
  return connection;
};

const backend = (): Backend => window.go?.main?.App ?? {
  ...mockBackend,
  ConnectBoard: (id: string) => withMockHistoryCapability(mockBackend.ConnectBoard(id)),
  RefreshBoard: (id: string) => withMockHistoryCapability(mockBackend.RefreshBoard(id)),
};

export const isMock = () => !window.go?.main?.App;
export const api = {
  appVersion: (): Promise<string> => backend().GetAppVersion(),
  checkStudioUpdate: (): Promise<StudioUpdateCheck> => backend().CheckStudioUpdate(),
  openStudioUpdate: () => backend().OpenStudioUpdate(),
  listBoards: (): Promise<Board[]> => backend().ListBoards(),
  probeBoard: (board: Board): Promise<Connection> => backend().ProbeBoard(board),
  saveBoard: (board: Board): Promise<Board> => backend().SaveBoard(board),
  removeBoard: (id: string) => backend().RemoveBoard(id),
  connectBoard: (id: string): Promise<Connection> => backend().ConnectBoard(id),
  refreshBoard: (id: string): Promise<Connection> => backend().RefreshBoard(id),
  systemSnapshot: (id: string): Promise<SystemSnapshot> => backend().GetSystemSnapshot(id),
  checkBoardUpdate: (id: string): Promise<BoardUpdateCheck> => backend().CheckBoardUpdate(id),
  installBoardUpdate: (id: string): Promise<BoardUpdateResult> => backend().InstallBoardUpdate(id),
  installBoardService: (board: Board): Promise<BoardInstallResult> => backend().InstallBoardService(board),
  saveSupportBundle: (id: string): Promise<SupportBundle> => backend().SaveSupportBundle(id),

  diagnostics: (id: string): Promise<DiagnosticReport> => backend().GetDiagnostics(id),
  repairDiagnostics: (id: string, action: string, confirmed: boolean): Promise<DiagnosticReport> => backend().RepairDiagnostics(id, action, confirmed),
  inspectDeployment: (boardId: string, cwd: string): Promise<DeploymentInspection> => backend().InspectDeployment(boardId, cwd),
  startDeployment: (boardId: string, request: StartDeploymentRequest): Promise<Task> => backend().StartDeployment(boardId, request),
  deploymentStatus: (boardId: string, taskId: string): Promise<DeploymentStatus> => backend().GetDeploymentStatus(boardId, taskId),
  inspectBPUModel: (boardId: string, modelPath: string): Promise<BPUModelInfo> => backend().InspectBPUModel(boardId, modelPath),
  runBPUBenchmark: (boardId: string, request: BPUBenchmarkRequest): Promise<BPUBenchmarkResult> => backend().RunBPUBenchmark(boardId, request),
  listWorkspaceBPUModels: (boardId: string, cwd: string): Promise<string[]> => backend().ListWorkspaceBPUModels(boardId, cwd),
  downloadSampleBPUModel: (boardId: string, soc: string): Promise<string> => backend().DownloadSampleBPUModel(boardId, soc),
  uploadBPUModel: (boardId: string, filename: string, base64Data: string): Promise<string> => backend().UploadBPUModel(boardId, filename, base64Data),
  deleteBPUModel: (boardId: string, modelPath: string): Promise<void> => backend().DeleteBPUModel(boardId, modelPath),
  disconnectBoard: (id: string) => backend().DisconnectBoard(id),



  tasks: (id: string, archived = false): Promise<TaskPage> => backend().RefreshTasks(id, archived),
  task: (boardId: string, taskId: string): Promise<Task> => backend().GetTask(boardId, taskId),
  schedules: (boardId: string, includeAll = true): Promise<Schedule[]> => backend().ListSchedules(boardId, includeAll),
  createSchedule: (boardId: string, request: CreateScheduleRequest): Promise<Schedule> => backend().CreateSchedule(boardId, request),
  pauseSchedule: (boardId: string, id: string): Promise<Schedule> => backend().PauseSchedule(boardId, id),
  resumeSchedule: (boardId: string, id: string): Promise<Schedule> => backend().ResumeSchedule(boardId, id),
  runSchedule: (boardId: string, id: string): Promise<Schedule> => backend().RunSchedule(boardId, id),
  deleteSchedule: (boardId: string, id: string) => backend().DeleteSchedule(boardId, id),
  events: (boardId: string, taskId: string, after = 0, limit = 200): Promise<EventPage> => backend().GetEvents(boardId, taskId, after, limit),
	beforeEvents: (boardId: string, taskId: string, before = 0, limit = 200): Promise<EventPage> => backend().GetEventsBefore(boardId, taskId, before, limit),
  startTask: (boardId: string, request: {name: string; cwd: string; prompt: string; images?: ImageContent[]; approve: boolean; model?: string; approvalModel?: string; permissionMode?: string; workspaceMode?: 'shared' | 'worktree'; sandboxMode?: 'review' | 'workspace' | 'system' | 'off'; networkMode?: 'shared' | 'model-only' | 'offline'}): Promise<Task> => backend().StartTask(boardId, request),
  sendPrompt: (boardId: string, taskId: string, prompt: string, images: ImageContent[] = []) => backend().SendPrompt(boardId, taskId, prompt, images),
  abortTask: (boardId: string, taskId: string) => backend().AbortTask(boardId, taskId),
  stopTask: (boardId: string, taskId: string) => backend().StopTask(boardId, taskId),
  deleteTasks: (boardId: string, taskIds: string[]) => backend().DeleteTasks(boardId, taskIds),
  resumeTask: (boardId: string, taskId: string, prompt: string, images: ImageContent[] = []): Promise<Task> => backend().ResumeTask(boardId, taskId, prompt, images),
  restartTask: (boardId: string, taskId: string, prompt: string, images: ImageContent[] = []): Promise<Task> => backend().RestartTask(boardId, taskId, prompt, images),
  setModel: (boardId: string, taskId: string, provider: string, modelId: string) => backend().SetTaskModel(boardId, taskId, provider, modelId),
  setPermissionMode: (boardId: string, taskId: string, mode: string): Promise<Task> => backend().SetTaskPermissionMode(boardId, taskId, mode),
  setApprovalModel: (boardId: string, taskId: string, model: string): Promise<Task> => backend().SetTaskApprovalModel(boardId, taskId, model),
  setSandboxMode: (boardId: string, taskId: string, mode: string): Promise<Task> => backend().SetTaskSandboxMode(boardId, taskId, mode),
  setNetworkMode: (boardId: string, taskId: string, mode: string): Promise<Task> => backend().SetTaskNetworkMode(boardId, taskId, mode),
  renameTask: (boardId: string, taskId: string, name: string): Promise<Task> => backend().RenameTask(boardId, taskId, name),
  models: (boardId: string): Promise<ModelOption[]> => backend().ListModels(boardId),
  providers: (boardId: string): Promise<ManagedProvider[]> => backend().ListManagedProviders(boardId),
  addProvider: (boardId: string, request: AddManagedProviderRequest, apiKey: string): Promise<ProviderMutationResult> => backend().AddManagedProvider(boardId, request, apiKey),
  rotateProvider: (boardId: string, providerId: string, apiKey: string, allowShared = false): Promise<ProviderMutationResult> => backend().RotateManagedProviderCredential(boardId, providerId, apiKey, allowShared),
  removeProvider: (boardId: string, providerId: string, keepCredential = false): Promise<ProviderMutationResult> => backend().RemoveManagedProvider(boardId, providerId, keepCredential),
  applyProviderConfiguration: (boardId: string): Promise<ProviderMutationResult> => backend().ApplyProviderConfiguration(boardId),
  extensions: (boardId: string, taskId = ''): Promise<ExtensionCatalog> => backend().ListExtensions(boardId, taskId),
  modelHealth: (boardId: string, model: string, force = false): Promise<ModelHealth> => backend().CheckModelHealth(boardId, model, force),
  modelConformance: (boardId: string, model: string, force = false): Promise<ModelConformance> => backend().VerifyModel(boardId, model, force),
  modelRuntimeProbe: (boardId: string, model: string): Promise<ModelRuntimeProbe> => backend().ProbeModelRuntime(boardId, model),
  modelRDKProbe: (boardId: string, model: string, profile = 'read-only-rdk-diagnostic-v1'): Promise<ModelRDKProbe> => backend().ProbeModelRDK(boardId, model, profile),
  modelRDKMatrix: (boardId: string, model: string): Promise<ModelRDKMatrix> => backend().GetModelRDKMatrix(boardId, model),
  modelQualification: (boardId: string, model: string): Promise<ModelQualification> => backend().GetModelQualification(boardId, model),
  browseWorkspace: (boardId: string, path = ''): Promise<WorkspaceListing> => backend().BrowseWorkspace(boardId, path),
  createWorkspace: (boardId: string, parent: string, name: string): Promise<WorkspaceListing> => backend().CreateWorkspace(boardId, parent, name),
  workspaceChanges: (boardId: string, taskId: string): Promise<WorkspaceChanges> => backend().GetWorkspaceChanges(boardId, taskId),
  inspectWorkspaceIsolation: (boardId: string, path: string): Promise<WorkspaceIsolation> => backend().InspectWorkspaceIsolation(boardId, path),
  inspectWorkspaceDelivery: (boardId: string, taskId: string): Promise<WorkspaceDelivery> => backend().InspectWorkspaceDelivery(boardId, taskId),
  applyWorkspace: (boardId: string, taskId: string, expectedDigest: string): Promise<WorkspaceApplyResult> => backend().ApplyWorkspace(boardId, taskId, expectedDigest),
  forkTask: (boardId: string, request: ForkTaskRequest): Promise<Task> => backend().ForkTask(boardId, request),
  respond: (boardId: string, taskId: string, approvalId: string, response: Record<string, unknown>) => backend().RespondApproval(boardId, taskId, approvalId, response),
  watch: (boardId: string, taskId: string, after: number) => backend().WatchTask(boardId, taskId, after),
  stopWatch: (boardId: string, taskId: string) => backend().StopWatchingTask(boardId, taskId),
  openExternalURL: (url: string) => backend().OpenExternalURL(url),
  onEvent: (callback: (envelope: EventEnvelope) => void) => window.runtime?.EventsOn?.('task:event', callback) ?? (() => undefined),
  onWatchStatus: (callback: (status: TaskWatchStatus) => void) => window.runtime?.EventsOn?.('task:watch-status', callback) ?? (() => undefined),
  onWatchError: (callback: (error: {boardId: string; taskId: string; error: string}) => void) => window.runtime?.EventsOn?.('task:watch-error', callback) ?? (() => undefined),
};
