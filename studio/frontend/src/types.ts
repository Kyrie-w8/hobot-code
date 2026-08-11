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
  sessionId?: string;
  resumeCount?: number;
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
