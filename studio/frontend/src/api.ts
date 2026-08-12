import type {Board, Connection, DeploymentInspection, DeploymentStatus, EventEnvelope, EventPage, ForkTaskRequest, ImageContent, ModelHealth, ModelOption, StartDeploymentRequest, SupportBundle, SystemSnapshot, Task, TaskPage, WorkspaceListing} from './types';

export type TaskWatchStatus = {boardId: string; taskId: string; state: 'connected' | 'reconnecting' | 'failed'; attempt?: number; message?: string};

type Backend = Record<string, (...args: any[]) => Promise<any>>;

declare global {
  interface Window {
    go?: { main?: { App?: Backend } };
    runtime?: {
      EventsOn?: (name: string, callback: (...args: any[]) => void) => () => void;
      WindowToggleMaximise?: () => void;
    };
  }
}

const mockBoard: Board = {id: 's600-demo', name: 'RDK S600', host: 'rdk-s600.local', user: 'root', port: 22};
const now = new Date();
const mockTasks: Task[] = [
  {id: 'b8930da1a77e4a8f12345678', name: 'model-benchmark', cwd: '/root/yolo_bench_s100', status: 'idle', pid: 842107, createdAt: new Date(now.getTime() - 42 * 60_000).toISOString(), updatedAt: now.toISOString(), lastSequence: 128, sessionId: '019fef9b-695f-7e9d', sessionFile: '/root/.local/state/hobot-code/sessions/demo.jsonl', model: 'drobotics/kimi-k3'},
  {id: '04acf83b820b934e12345678', name: 'camera-pipeline', cwd: '/root/tros_ws', status: 'waiting', pid: 842244, createdAt: new Date(now.getTime() - 18 * 60_000).toISOString(), updatedAt: now.toISOString(), lastSequence: 72, pendingApprovals: [{id: 'approval-demo', method: 'select', title: 'Allow bash?\nRisk: Executes a shell command with the current user privileges.\nTarget: hobot-board info', message: 'Choose how Hobot Code may run this tool.', options: ['Allow once', 'Allow this exact call for this task', 'Deny'], active: true}]},
  {id: 'f30bb47e8d552f1812345678', name: 'deploy-review', cwd: '/root/yolo_bench_s100', status: 'idle', pid: 841992, createdAt: new Date(now.getTime() - 70 * 60_000).toISOString(), updatedAt: now.toISOString(), lastSequence: 53, parentTaskId: 'b8930da1a77e4a8f12345678', branchKind: 'side', model: 'drobotics/kimi-k3'},
];

const mockEvents = (taskId: string): EventPage => ({
  events: [
    {protocol: 1, kind: 'event', taskId, sequence: 120, time: new Date(now.getTime() - 55_000).toISOString(), event: {type: 'hobot_user_prompt'}, normalized: {schema: 3, type: 'user.message', data: {text: 'Inspect the S100 runtime, then run a final latency pass.'}}},
    {protocol: 1, kind: 'event', taskId, sequence: 121, time: new Date(now.getTime() - 48_000).toISOString(), event: {type: 'message_update'}, normalized: {schema: 3, type: 'assistant.thinking.delta', data: {delta: 'Inspecting the BPU runtime and benchmark workspace before changing the deployment configuration.'}}},
    {protocol: 1, kind: 'event', taskId, sequence: 122, time: new Date(now.getTime() - 36_000).toISOString(), event: {type: 'tool_execution_start'}, normalized: {schema: 3, type: 'tool.started', data: {toolCallId: 'tool-1', toolName: 'bash'}}},
    {protocol: 1, kind: 'event', taskId, sequence: 123, time: new Date(now.getTime() - 21_000).toISOString(), event: {type: 'tool_execution_end'}, normalized: {schema: 3, type: 'tool.completed', data: {toolCallId: 'tool-1', toolName: 'bash', isError: false}}},
    {protocol: 1, kind: 'event', taskId, sequence: 124, time: new Date(now.getTime() - 8_000).toISOString(), event: {type: 'message_update'}, normalized: {schema: 3, type: 'assistant.text.delta', data: {delta: 'The S100 runtime is healthy. The final latency pass completed successfully.'}}},
    {protocol: 1, kind: 'event', taskId, sequence: 125, time: new Date(now.getTime() - 2_000).toISOString(), event: {type: 'task.idle'}, normalized: {schema: 3, type: 'task.idle'}},
  ],
  nextAfter: 125,
  hasMore: false,
});

const mockBackend: Backend = {
  ListBoards: async () => [mockBoard],
  SaveBoard: async (board: Board) => ({...board, id: board.id || `board-${Date.now()}`}),
  RemoveBoard: async () => undefined,
  ConnectBoard: async (id: string): Promise<Connection> => ({board: {...mockBoard, id}, connected: true, daemon: {version: '0.25.0', pid: 834124, startedAt: now.toISOString(), activeTasks: 3, maximumTasks: 3, stateRoot: '/root/.local/state/hobot-code'}, capabilities: {protocolMin: 1, protocolMax: 1, eventSchema: 3, capabilities: ['events.normalized.v3', 'system.snapshot', 'support.bundle.v1', 'deployments.v1', 'tasks.lifecycle', 'tasks.page', 'events.page', 'tasks.resume', 'tasks.restart', 'tasks.fork', 'tasks.models', 'tasks.permissions', 'tasks.images', 'models.capabilities.v1', 'models.health.v1', 'workspaces.browse', 'bridge.stdio'], maximumActiveTasks: 3, maximumRetainedTasks: 100}, compatibility: {status: 'supported', summary: 'Board and Studio capabilities are compatible.', appVersion: '0.25.0', agentdVersion: '0.25.0', protocol: 1, eventSchema: 3, boardId: 's600', rdkOsVersion: '5.1.0', validatedTarget: true, issues: []}}),
  RefreshBoard: async (id: string): Promise<Connection> => ({board: {...mockBoard, id}, connected: true, daemon: {version: '0.24.0', pid: 834124, startedAt: now.toISOString(), activeTasks: 3, maximumTasks: 3, stateRoot: '/root/.local/state/hobot-code'}, capabilities: {protocolMin: 1, protocolMax: 1, eventSchema: 3, capabilities: ['events.normalized.v3', 'system.snapshot', 'tasks.lifecycle', 'tasks.page', 'events.page'], maximumActiveTasks: 3, maximumRetainedTasks: 100}, compatibility: {status: 'limited', summary: 'Connected with limited features.', appVersion: '0.25.0', agentdVersion: '0.24.0', protocol: 1, eventSchema: 3, boardId: 's600', rdkOsVersion: '5.1.0', validatedTarget: true, issues: [{code: 'version-line-mismatch', severity: 'warning', message: 'Studio and board service are from different release lines.', action: 'Update Hobot Code on the board.'}]}}),
  GetSystemSnapshot: async (): Promise<SystemSnapshot> => ({capturedAt: new Date().toISOString(), board: 'D-Robotics RDK S600', boardId: 's600', hostname: 'drobot', rdkOsVersion: '5.1.0', kernel: '6.1.158-rt58', architecture: 'arm64', cpuCores: 18, loadAverage: [6.18, 5.72, 5.4], memory: {totalBytes: 11 * 1024 ** 3, availableBytes: 4.4 * 1024 ** 3}, disk: {path: '/root/.local/state/hobot-code', totalBytes: 220 * 1024 ** 3, availableBytes: 188 * 1024 ** 3}, thermalZones: [{name: 'pvt_cmn', celsius: 44.2}, {name: 'pvt_bpu0', celsius: 43.1}, {name: 'pvt_bpu1', celsius: 43.8}], bpuDevices: ['/dev/bpu', '/dev/bpu_core0', '/dev/bpu_core1', '/dev/bpu_core2', '/dev/bpu_core3'], bpuCores: [{index: 0, name: 'BPU 0', utilizationPercent: 68, currentFrequencyHz: 1_500_000_000, maximumFrequencyHz: 1_500_000_000}, {index: 1, name: 'BPU 1', utilizationPercent: 42, currentFrequencyHz: 1_500_000_000, maximumFrequencyHz: 1_500_000_000}, {index: 2, name: 'BPU 2', utilizationPercent: 17, currentFrequencyHz: 1_333_330_000, maximumFrequencyHz: 1_500_000_000}, {index: 3, name: 'BPU 3', utilizationPercent: 5, currentFrequencyHz: 1_333_330_000, maximumFrequencyHz: 1_500_000_000}], aiMemory: {available: true, bpuAllocationAvailable: true, ionAvailable: true, cmaAvailable: false, dmaBufAvailable: true, bpuAllocatedBytes: 384 * 1024 ** 2, ionAllocatedBytes: 712 * 1024 ** 2, ionOrphanedBytes: 0, dmaBufBytes: 96 * 1024 ** 2, dmaBufObjects: 18}, accelerator: {available: true, source: 'ion-debugfs', capturedAt: new Date().toISOString(), ddrReadMiBps: 324, ddrWriteMiBps: 118, hbmemPools: [{name: 'cma_reserved', totalBytes: 1024 ** 3, usedBytes: 384 * 1024 ** 2, freeBytes: 640 * 1024 ** 2, processBytes: 96 * 1024 ** 2, systemBytes: 288 * 1024 ** 2}, {name: 'ion_cma', totalBytes: 512 * 1024 ** 2, usedBytes: 96 * 1024 ** 2, freeBytes: 416 * 1024 ** 2, processBytes: 32 * 1024 ** 2, systemBytes: 64 * 1024 ** 2}, {name: 'carveout', totalBytes: 512 * 1024 ** 2, usedBytes: 242 * 1024 ** 2, freeBytes: 270 * 1024 ** 2, processBytes: 200 * 1024 ** 2, systemBytes: 42 * 1024 ** 2}], processes: [{pid: 18422, name: 'hrt_model_exec', rssBytes: 512 * 1024 ** 2, hbmemBytes: 328 * 1024 ** 2}]}, rdkUtilities: {hrut_somstatus: true, hrt_model_exec: true, rdkos_info: true}, uptimeSeconds: 186420}),
  SaveSupportBundle: async (): Promise<SupportBundle> => ({id: 'demo-support', createdAt: new Date().toISOString(), path: '/Users/demo/Downloads/hobot-code-support-demo.json', sizeBytes: 4096, sha256: 'demo', excluded: ['prompts'], checks: {pass: 8, warn: 1, fail: 0}}),
  InspectDeployment: async (_board: string, cwd: string): Promise<DeploymentInspection> => ({capturedAt: new Date().toISOString(), cwd, board: 'D-Robotics RDK S600', boardId: 's600', rdkOsVersion: '5.1.0', truncated: false, artifacts: [{path: `${cwd}/model.onnx`, relativePath: 'model.onnx', name: 'model.onnx', kind: 'onnx', sizeBytes: 48 * 1024 ** 2, modifiedAt: new Date().toISOString(), compatibility: 'conversion-required', reason: 'source model requires board-specific conversion and quantization'}, {path: `${cwd}/output/detector_s600.hbm`, relativePath: 'output/detector_s600.hbm', name: 'detector_s600.hbm', kind: 'rdk-hbm', sizeBytes: 23 * 1024 ** 2, modifiedAt: new Date().toISOString(), compatibility: 'candidate', reason: 'filename matches the current board; runtime validation is still required'}]}),
  StartDeployment: async (_board: string, request: StartDeploymentRequest): Promise<Task> => ({...mockTasks[0], id: `task-${Date.now()}`, name: request.name || `Deploy ${request.artifactPath.split('/').at(-1)}`, cwd: request.cwd, status: 'starting'}),
  GetDeploymentStatus: async (_board: string, taskId: string): Promise<DeploymentStatus> => ({taskId, phase: 'running', deployment: {schema: 1, cwd: '/root/yolo_bench_s100', board: 'D-Robotics RDK S600', boardId: 's600', rdkOsVersion: '5.1.0', goal: 'deploy-and-validate', artifact: {path: '/root/yolo_bench_s100/model.onnx', relativePath: 'model.onnx', name: 'model.onnx', kind: 'onnx', sizeBytes: 1, modifiedAt: new Date().toISOString(), compatibility: 'conversion-required', reason: 'conversion required'}, reportPath: '/root/yolo_bench_s100/.hobot/deployments/demo.json', createdAt: new Date().toISOString()}}),
  DisconnectBoard: async () => undefined,
  RefreshTasks: async (): Promise<TaskPage> => ({tasks: mockTasks}),
  GetTask: async (_board: string, taskId: string) => mockTasks.find((task) => task.id === taskId),
  GetEvents: async (_board: string, taskId: string) => mockEvents(taskId),
  StartTask: async (_board: string, request: any) => ({...mockTasks[2], id: `task-${Date.now()}`, name: request.name || 'new-task', cwd: request.cwd, status: 'starting'}),
  SendPrompt: async () => undefined,
  StopTask: async () => undefined,
  DeleteTasks: async () => undefined,
  ResumeTask: async (_board: string, taskId: string) => ({...mockTasks.find((task) => task.id === taskId), status: 'starting'}),
  RestartTask: async (_board: string, taskId: string) => ({...mockTasks.find((task) => task.id === taskId), status: 'starting', sessionFile: undefined, sessionId: undefined}),
  SetTaskModel: async () => undefined,
  SetTaskPermissionMode: async (_board: string, taskId: string, mode: string) => ({...mockTasks.find((task) => task.id === taskId), permissionMode: mode}),
  RenameTask: async (_board: string, taskId: string, name: string) => ({...mockTasks.find((task) => task.id === taskId), name}),
  ListModels: async (): Promise<ModelOption[]> => [
    {provider: 'drobotics', id: 'kimi-k3', name: 'kimi-k3', default: true, capabilities: {reasoning: true, imageInput: true}, capabilitySource: 'runtime-model-table'},
    {provider: 'drobotics', id: 'qwen3.8-max', name: 'qwen3.8-max', capabilities: {reasoning: true, imageInput: true}, capabilitySource: 'runtime-model-table'},
    {provider: 'drobotics', id: 'glm-5.2', name: 'glm-5.2', capabilities: {reasoning: true, imageInput: true}, capabilitySource: 'runtime-model-table'},
    {provider: 'drobotics', id: 'deepseek-v4-flash', name: 'deepseek-v4-flash', capabilities: {reasoning: true, imageInput: false}, capabilitySource: 'runtime-model-table'},
    {provider: 'drobotics', id: 'deepseek-v4-pro', name: 'deepseek-v4-pro', capabilities: {reasoning: true, imageInput: false}, capabilitySource: 'runtime-model-table'},
  ],
  CheckModelHealth: async (_board: string, model: string): Promise<ModelHealth> => ({provider: model.split('/')[0], model: model.split('/').slice(1).join('/'), status: 'available', category: 'ok', message: 'The model gateway completed a minimal response successfully.', transport: 'sse', checkedAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 300_000).toISOString(), firstByteMs: 326, latencyMs: 612, attempts: 1, cached: false}),
  BrowseWorkspace: async (_board: string, path: string): Promise<WorkspaceListing> => ({path: path || '/root', parent: path === '/root' || !path ? '/' : '/root', home: '/root', directories: [{name: 'models', path: '/root/models'}, {name: 'tros_ws', path: '/root/tros_ws'}, {name: 'yolo_bench_s100', path: '/root/yolo_bench_s100'}]}),
  CreateWorkspace: async (_board: string, parent: string, name: string): Promise<WorkspaceListing> => ({path: `${parent}/${name}`.replace('//', '/'), parent, home: '/root', directories: []}),
  ForkTask: async (_board: string, request: ForkTaskRequest) => ({...mockTasks[2], id: `task-${Date.now()}`, name: request.name || `${mockTasks[0].name}-side`, status: 'starting', parentTaskId: request.taskId, forkSequence: request.sequence, branchKind: request.kind}),
  RespondApproval: async () => undefined,
  WatchTask: async () => undefined,
  StopWatchingTask: async () => undefined,
  OpenExternalURL: async (url: string) => { window.open(url, '_blank', 'noopener,noreferrer'); },
};

const backend = (): Backend => window.go?.main?.App ?? mockBackend;

export const isMock = () => !window.go?.main?.App;
export const api = {
  listBoards: (): Promise<Board[]> => backend().ListBoards(),
  saveBoard: (board: Board): Promise<Board> => backend().SaveBoard(board),
  removeBoard: (id: string) => backend().RemoveBoard(id),
  connectBoard: (id: string): Promise<Connection> => backend().ConnectBoard(id),
  refreshBoard: (id: string): Promise<Connection> => backend().RefreshBoard(id),
  systemSnapshot: (id: string): Promise<SystemSnapshot> => backend().GetSystemSnapshot(id),
  saveSupportBundle: (id: string): Promise<SupportBundle> => backend().SaveSupportBundle(id),
  inspectDeployment: (boardId: string, cwd: string): Promise<DeploymentInspection> => backend().InspectDeployment(boardId, cwd),
  startDeployment: (boardId: string, request: StartDeploymentRequest): Promise<Task> => backend().StartDeployment(boardId, request),
  deploymentStatus: (boardId: string, taskId: string): Promise<DeploymentStatus> => backend().GetDeploymentStatus(boardId, taskId),
  disconnectBoard: (id: string) => backend().DisconnectBoard(id),
  tasks: (id: string, archived = false): Promise<TaskPage> => backend().RefreshTasks(id, archived),
  task: (boardId: string, taskId: string): Promise<Task> => backend().GetTask(boardId, taskId),
  events: (boardId: string, taskId: string, after = 0, limit = 200): Promise<EventPage> => backend().GetEvents(boardId, taskId, after, limit),
  startTask: (boardId: string, request: {name: string; cwd: string; prompt: string; images?: ImageContent[]; approve: boolean; model?: string; permissionMode?: string}): Promise<Task> => backend().StartTask(boardId, request),
  sendPrompt: (boardId: string, taskId: string, prompt: string, images: ImageContent[] = []) => backend().SendPrompt(boardId, taskId, prompt, images),
  stopTask: (boardId: string, taskId: string) => backend().StopTask(boardId, taskId),
  deleteTasks: (boardId: string, taskIds: string[]) => backend().DeleteTasks(boardId, taskIds),
  resumeTask: (boardId: string, taskId: string, prompt: string, images: ImageContent[] = []): Promise<Task> => backend().ResumeTask(boardId, taskId, prompt, images),
  restartTask: (boardId: string, taskId: string, prompt: string, images: ImageContent[] = []): Promise<Task> => backend().RestartTask(boardId, taskId, prompt, images),
  setModel: (boardId: string, taskId: string, provider: string, modelId: string) => backend().SetTaskModel(boardId, taskId, provider, modelId),
  setPermissionMode: (boardId: string, taskId: string, mode: string): Promise<Task> => backend().SetTaskPermissionMode(boardId, taskId, mode),
  renameTask: (boardId: string, taskId: string, name: string): Promise<Task> => backend().RenameTask(boardId, taskId, name),
  models: (boardId: string): Promise<ModelOption[]> => backend().ListModels(boardId),
  modelHealth: (boardId: string, model: string, force = false): Promise<ModelHealth> => backend().CheckModelHealth(boardId, model, force),
  browseWorkspace: (boardId: string, path = ''): Promise<WorkspaceListing> => backend().BrowseWorkspace(boardId, path),
  createWorkspace: (boardId: string, parent: string, name: string): Promise<WorkspaceListing> => backend().CreateWorkspace(boardId, parent, name),
  forkTask: (boardId: string, request: ForkTaskRequest): Promise<Task> => backend().ForkTask(boardId, request),
  respond: (boardId: string, taskId: string, approvalId: string, response: Record<string, unknown>) => backend().RespondApproval(boardId, taskId, approvalId, response),
  watch: (boardId: string, taskId: string, after: number) => backend().WatchTask(boardId, taskId, after),
  stopWatch: (boardId: string, taskId: string) => backend().StopWatchingTask(boardId, taskId),
  openExternalURL: (url: string) => backend().OpenExternalURL(url),
  onEvent: (callback: (envelope: EventEnvelope) => void) => window.runtime?.EventsOn?.('task:event', callback) ?? (() => undefined),
  onWatchStatus: (callback: (status: TaskWatchStatus) => void) => window.runtime?.EventsOn?.('task:watch-status', callback) ?? (() => undefined),
  onWatchError: (callback: (error: {boardId: string; taskId: string; error: string}) => void) => window.runtime?.EventsOn?.('task:watch-error', callback) ?? (() => undefined),
};
