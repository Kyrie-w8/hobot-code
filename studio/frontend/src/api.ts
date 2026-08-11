import type {Board, Connection, EventEnvelope, EventPage, Task, TaskPage} from './types';

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
  {id: 'b8930da1a77e4a8f12345678', name: 'model-benchmark', cwd: '/root/yolo_bench_s100', status: 'running', pid: 842107, createdAt: new Date(now.getTime() - 42 * 60_000).toISOString(), updatedAt: now.toISOString(), lastSequence: 128, sessionId: '019fef9b-695f-7e9d'},
  {id: '04acf83b820b934e12345678', name: 'camera-pipeline', cwd: '/root/tros_ws', status: 'waiting', pid: 842244, createdAt: new Date(now.getTime() - 18 * 60_000).toISOString(), updatedAt: now.toISOString(), lastSequence: 72, pendingApprovals: [{id: 'approval-demo', method: 'confirm', title: 'Allow bash?', message: 'Run the camera device inspection command on RDK S100.', active: true}]},
  {id: 'f30bb47e8d552f1812345678', name: 'deploy-review', cwd: '/root/models', status: 'idle', pid: 841992, createdAt: new Date(now.getTime() - 70 * 60_000).toISOString(), updatedAt: now.toISOString(), lastSequence: 53},
];

const mockEvents = (taskId: string): EventPage => ({
  events: [
    {protocol: 1, kind: 'event', taskId, sequence: 121, time: new Date(now.getTime() - 48_000).toISOString(), event: {type: 'message_update'}, normalized: {schema: 2, type: 'assistant.thinking.delta', data: {delta: 'Inspecting the BPU runtime and benchmark workspace before changing the deployment configuration.'}}},
    {protocol: 1, kind: 'event', taskId, sequence: 122, time: new Date(now.getTime() - 36_000).toISOString(), event: {type: 'tool_execution_start'}, normalized: {schema: 2, type: 'tool.started', data: {toolCallId: 'tool-1', toolName: 'bash'}}},
    {protocol: 1, kind: 'event', taskId, sequence: 123, time: new Date(now.getTime() - 21_000).toISOString(), event: {type: 'tool_execution_end'}, normalized: {schema: 2, type: 'tool.completed', data: {toolCallId: 'tool-1', toolName: 'bash', isError: false}}},
    {protocol: 1, kind: 'event', taskId, sequence: 124, time: new Date(now.getTime() - 8_000).toISOString(), event: {type: 'message_update'}, normalized: {schema: 2, type: 'assistant.text.delta', data: {delta: 'The S100 runtime is healthy. I am running the final latency pass now.'}}},
    {protocol: 1, kind: 'event', taskId, sequence: 125, time: new Date(now.getTime() - 2_000).toISOString(), event: {type: 'task.running'}, normalized: {schema: 2, type: 'task.running'}},
  ],
  nextAfter: 125,
  hasMore: false,
});

const mockBackend: Backend = {
  ListBoards: async () => [mockBoard],
  SaveBoard: async (board: Board) => ({...board, id: board.id || `board-${Date.now()}`}),
  RemoveBoard: async () => undefined,
  ConnectBoard: async (id: string): Promise<Connection> => ({board: {...mockBoard, id}, connected: true, daemon: {version: '0.17.0', pid: 834124, startedAt: now.toISOString(), activeTasks: 2, maximumTasks: 2, stateRoot: '/root/.local/state/hobot-code'}, capabilities: {protocolMin: 1, protocolMax: 1, eventSchema: 2, capabilities: ['events.normalized.v2', 'tasks.resume', 'bridge.stdio'], maximumActiveTasks: 2, maximumRetainedTasks: 200}}),
  DisconnectBoard: async () => undefined,
  RefreshTasks: async (): Promise<TaskPage> => ({tasks: mockTasks}),
  GetTask: async (_board: string, taskId: string) => mockTasks.find((task) => task.id === taskId),
  GetEvents: async (_board: string, taskId: string) => mockEvents(taskId),
  StartTask: async (_board: string, request: any) => ({...mockTasks[2], id: `task-${Date.now()}`, name: request.name || 'new-task', cwd: request.cwd, status: 'starting'}),
  SendPrompt: async () => undefined,
  StopTask: async () => undefined,
  ResumeTask: async (_board: string, taskId: string) => ({...mockTasks.find((task) => task.id === taskId), status: 'starting'}),
  RespondApproval: async () => undefined,
  WatchTask: async () => undefined,
  StopWatchingTask: async () => undefined,
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
  startTask: (boardId: string, request: {name: string; cwd: string; prompt: string; approve: boolean}): Promise<Task> => backend().StartTask(boardId, request),
  sendPrompt: (boardId: string, taskId: string, prompt: string) => backend().SendPrompt(boardId, taskId, prompt),
  stopTask: (boardId: string, taskId: string) => backend().StopTask(boardId, taskId),
  resumeTask: (boardId: string, taskId: string, prompt: string): Promise<Task> => backend().ResumeTask(boardId, taskId, prompt),
  respond: (boardId: string, taskId: string, approvalId: string, response: Record<string, unknown>) => backend().RespondApproval(boardId, taskId, approvalId, response),
  watch: (boardId: string, taskId: string, after: number) => backend().WatchTask(boardId, taskId, after),
  stopWatch: (boardId: string, taskId: string) => backend().StopWatchingTask(boardId, taskId),
  onEvent: (callback: (envelope: EventEnvelope) => void) => window.runtime?.EventsOn?.('task:event', callback) ?? (() => undefined),
  onWatchError: (callback: (error: {boardId: string; taskId: string; error: string}) => void) => window.runtime?.EventsOn?.('task:watch-error', callback) ?? (() => undefined),
};
