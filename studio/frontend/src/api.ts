import type {Board, Connection, EventEnvelope, EventPage, ForkTaskRequest, ImageContent, ModelOption, Task, TaskPage, WorkspaceListing} from './types';

type Backend = Record<string, (...args: any[]) => Promise<any>>;

declare global {
  interface Window {
    go?: { main?: { App?: Backend } };
    runtime?: { EventsOn?: (name: string, callback: (...args: any[]) => void) => () => void };
  }
}

const mockBoard: Board = {id: 's100-demo', name: 'RDK S100', host: '10.112.10.98', user: 'root', port: 22};
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
  ConnectBoard: async (id: string): Promise<Connection> => ({board: {...mockBoard, id}, connected: true, daemon: {version: '0.22.2', pid: 834124, startedAt: now.toISOString(), activeTasks: 3, maximumTasks: 3, stateRoot: '/root/.local/state/hobot-code'}, capabilities: {protocolMin: 1, protocolMax: 1, eventSchema: 3, capabilities: ['events.normalized.v3', 'tasks.resume', 'tasks.restart', 'tasks.fork', 'tasks.models', 'tasks.permissions', 'tasks.images', 'workspaces.browse', 'bridge.stdio'], maximumActiveTasks: 3, maximumRetainedTasks: 100}}),
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
    {provider: 'drobotics', id: 'kimi-k3', name: 'kimi-k3'},
    {provider: 'drobotics', id: 'qwen3.8-max', name: 'qwen3.8-max'},
    {provider: 'drobotics', id: 'glm-5.2', name: 'glm-5.2'},
  ],
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
  browseWorkspace: (boardId: string, path = ''): Promise<WorkspaceListing> => backend().BrowseWorkspace(boardId, path),
  createWorkspace: (boardId: string, parent: string, name: string): Promise<WorkspaceListing> => backend().CreateWorkspace(boardId, parent, name),
  forkTask: (boardId: string, request: ForkTaskRequest): Promise<Task> => backend().ForkTask(boardId, request),
  respond: (boardId: string, taskId: string, approvalId: string, response: Record<string, unknown>) => backend().RespondApproval(boardId, taskId, approvalId, response),
  watch: (boardId: string, taskId: string, after: number) => backend().WatchTask(boardId, taskId, after),
  stopWatch: (boardId: string, taskId: string) => backend().StopWatchingTask(boardId, taskId),
  openExternalURL: (url: string) => backend().OpenExternalURL(url),
  onEvent: (callback: (envelope: EventEnvelope) => void) => window.runtime?.EventsOn?.('task:event', callback) ?? (() => undefined),
  onWatchError: (callback: (error: {boardId: string; taskId: string; error: string}) => void) => window.runtime?.EventsOn?.('task:watch-error', callback) ?? (() => undefined),
};
