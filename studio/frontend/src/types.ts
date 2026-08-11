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
  daemon?: DaemonInfo;
  capabilities?: Capabilities;
  error?: string;
};

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
  sessionFile?: string;
  sessionId?: string;
  approved?: boolean;
  resumeCount?: number;
  restartCount?: number;
  model?: string;
  parentTaskId?: string;
  forkSequence?: number;
  branchKind?: 'side' | 'edit';
  archivedAt?: string;
  pendingApprovals?: Approval[];
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
export type WorkspaceEntry = {name: string; path: string};
export type WorkspaceListing = {path: string; parent?: string; home: string; directories: WorkspaceEntry[]};
export type ForkTaskRequest = {taskId: string; sequence?: number; prompt: string; name?: string; kind: 'side' | 'edit'; model?: string};
