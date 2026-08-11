import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import type {FormEvent, ReactNode} from 'react';
import {
  Activity, Bot, Box, Check, ChevronDown, CircleStop, Clipboard, Cpu,
  ListTodo, LoaderCircle, MemoryStick, MessageSquare, PanelRight, Play, Plus,
  RefreshCw, RotateCcw, Search, Server, ShieldCheck, SquareTerminal, X, XCircle,
} from 'lucide-react';
import {api, isMock} from './api';
import type {Approval, Board, Connection, Task, TaskEvent} from './types';
import './App.css';

const statusLabel: Record<string, string> = {
  starting: 'Starting', idle: 'Idle', running: 'Running', waiting: 'Approval',
  stopping: 'Stopping', stopped: 'Stopped', failed: 'Failed', interrupted: 'Interrupted',
};

const statusTone = (status: string) => `status status-${status}`;
const terminal = new Set(['stopped', 'failed', 'interrupted']);

function App() {
  const [boards, setBoards] = useState<Board[]>([]);
  const [connection, setConnection] = useState<Connection | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  const [events, setEvents] = useState<TaskEvent[]>([]);
  const [search, setSearch] = useState('');
  const [composer, setComposer] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [showBoard, setShowBoard] = useState(false);
  const [showNewTask, setShowNewTask] = useState(false);
  const [showInspector, setShowInspector] = useState(true);
  const startupStarted = useRef(false);

  const boardId = connection?.board.id ?? '';

  const refreshTasks = useCallback(async (targetBoard = boardId) => {
    if (!targetBoard) return;
    try {
      const page = await api.tasks(targetBoard);
      setTasks(page.tasks ?? []);
      setSelectedTask((current) => {
        if (!current) return page.tasks?.[0] ?? null;
        return page.tasks?.find((task) => task.id === current.id) ?? page.tasks?.[0] ?? null;
      });
    } catch (reason) {
      setError(String(reason));
    }
  }, [boardId]);

  const connect = useCallback(async (board: Board) => {
    setBusy(true);
    setError('');
    try {
      const next = await api.connectBoard(board.id);
      setConnection(next);
      await refreshTasks(board.id);
      setShowBoard(false);
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  }, [refreshTasks]);

  useEffect(() => {
    if (startupStarted.current) return;
    startupStarted.current = true;
    api.listBoards().then((saved) => {
      setBoards(saved);
      if (saved.length > 0) void connect(saved[0]);
      else setShowBoard(true);
    }).catch((reason) => setError(String(reason)));
  }, []);

  useEffect(() => {
    const removeEvent = api.onEvent(({boardId: eventBoard, event}) => {
      if (eventBoard !== boardId || event.taskId !== selectedTask?.id) return;
      setEvents((current) => current.some((item) => item.sequence === event.sequence) ? current : [...current, event]);
      if (event.normalized?.type === 'task.idle' || event.normalized?.type === 'approval.requested') {
        void refreshTasks();
      }
    });
    const removeError = api.onWatchError((watchError) => {
      if (watchError.boardId === boardId) setError(watchError.error);
    });
    return () => { removeEvent(); removeError(); };
  }, [boardId, refreshTasks, selectedTask?.id]);

  useEffect(() => {
    if (!boardId || !selectedTask) {
      setEvents([]);
      return;
    }
    let active = true;
    api.events(boardId, selectedTask.id).then((page) => {
      if (!active) return;
      setEvents(page.events ?? []);
      const after = page.nextAfter ?? page.events[page.events.length - 1]?.sequence ?? 0;
      return api.watch(boardId, selectedTask.id, after);
    }).catch((reason) => setError(String(reason)));
    return () => {
      active = false;
      void api.stopWatch(boardId, selectedTask.id);
    };
  }, [boardId, selectedTask?.id]);

  useEffect(() => {
    if (!boardId) return;
    const timer = window.setInterval(() => void refreshTasks(), 5000);
    return () => window.clearInterval(timer);
  }, [boardId, refreshTasks]);

  const visibleTasks = useMemo(() => {
    const query = search.trim().toLowerCase();
    return query ? tasks.filter((task) => `${task.name} ${task.cwd} ${task.status}`.toLowerCase().includes(query)) : tasks;
  }, [search, tasks]);

  const activeApproval = selectedTask?.pendingApprovals?.find((approval) => approval.active);
  const timelineEvents = useMemo(() => coalesceEvents(events), [events]);

  async function submitPrompt(event: FormEvent) {
    event.preventDefault();
    const prompt = composer.trim();
    if (!prompt || !selectedTask || !boardId) return;
    setBusy(true);
    setError('');
    try {
      if (terminal.has(selectedTask.status)) await api.resumeTask(boardId, selectedTask.id, prompt);
      else await api.sendPrompt(boardId, selectedTask.id, prompt);
      setComposer('');
      await refreshTasks();
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  async function stopTask() {
    if (!selectedTask || !boardId) return;
    setBusy(true);
    try {
      await api.stopTask(boardId, selectedTask.id);
      await refreshTasks();
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  async function respond(approval: Approval, response: Record<string, unknown>) {
    if (!selectedTask || !boardId) return;
    setBusy(true);
    try {
      await api.respond(boardId, selectedTask.id, approval.id, response);
      await refreshTasks();
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={`studio-shell ${showInspector ? '' : 'inspector-hidden'}`}>
      <header className="titlebar">
        <div className="brand-mark" aria-label="Hobot Code">H</div>
        <span className="product-name">Hobot Code</span>
        <button className="board-switcher" onClick={() => setShowBoard(true)} disabled={busy}>
          <span className="board-name">{connection?.board.name ?? 'Connect board'}</span>
          {connection && <span className="connection-dot" />}
          <ChevronDown size={14} />
        </button>
        <div className="titlebar-spacer" />
        {isMock() && <span className="preview-label">Preview</span>}
        <button className="icon-button" title="Refresh tasks" onClick={() => void refreshTasks()}><RefreshCw size={16} /></button>
        <button className={`icon-button ${showInspector ? 'active' : ''}`} title="Toggle inspector" onClick={() => setShowInspector((value) => !value)}><PanelRight size={17} /></button>
      </header>

      <nav className="activity-rail">
        <button className="rail-button active" title="Agent tasks"><Bot size={20} /></button>
        <div className="rail-spacer" />
        <button className="rail-button" title="Manage boards" onClick={() => setShowBoard(true)}><Server size={19} /></button>
      </nav>

      <aside className="task-sidebar">
        <div className="sidebar-heading">
          <div>
            <span className="section-label">Agent tasks</span>
            <span className="task-count">{tasks.length}</span>
          </div>
          <button className="icon-button compact" title="New task" onClick={() => setShowNewTask(true)} disabled={!connection}><Plus size={17} /></button>
        </div>
        <label className="search-field">
          <Search size={15} />
          <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Filter tasks" />
        </label>
        <div className="task-list">
          {visibleTasks.map((task) => (
            <button key={task.id} className={`task-row ${selectedTask?.id === task.id ? 'selected' : ''}`} onClick={() => setSelectedTask(task)}>
              <span className={`task-state-dot dot-${task.status}`} />
              <span className="task-row-main">
                <span className="task-row-name">{task.name}</span>
                <span className="task-row-path">{task.cwd}</span>
              </span>
              <span className="task-row-time">{relativeTime(task.updatedAt)}</span>
            </button>
          ))}
          {visibleTasks.length === 0 && <div className="empty-state"><ListTodo size={22} /><span>No tasks</span></div>}
        </div>
      </aside>

      <main className="task-main">
        {selectedTask ? <>
          <div className="task-header">
            <div className="task-title-block">
              <div className="task-title-line">
                <h1>{selectedTask.name}</h1>
                <span className={statusTone(selectedTask.status)}>{statusLabel[selectedTask.status] ?? selectedTask.status}</span>
              </div>
              <span className="workspace-path">{selectedTask.cwd}</span>
            </div>
            <div className="task-actions">
              {terminal.has(selectedTask.status) ? <button className="secondary-button" onClick={() => document.querySelector<HTMLTextAreaElement>('#composer')?.focus()}><RotateCcw size={15} /> Resume</button> :
                <button className="secondary-button" onClick={stopTask} disabled={busy || selectedTask.status === 'stopping'}><CircleStop size={15} /> Stop</button>}
            </div>
          </div>

          {activeApproval && <ApprovalBar approval={activeApproval} busy={busy} respond={(response) => respond(activeApproval, response)} />}

          <section className="timeline" aria-label="Agent activity">
            <div className="timeline-inner">
              {timelineEvents.map((event) => <TimelineEvent key={event.sequence} event={event} />)}
              {selectedTask.status === 'running' && <div className="working-row"><LoaderCircle size={15} className="spin" /><span>Working</span></div>}
              {events.length === 0 && <div className="empty-timeline"><MessageSquare size={24} /><span>No activity yet</span></div>}
            </div>
          </section>

          <form className="composer" onSubmit={submitPrompt}>
            <textarea id="composer" value={composer} onChange={(event) => setComposer(event.target.value)} placeholder={terminal.has(selectedTask.status) ? 'Resume this task with a new instruction' : 'Message the agent'} rows={2} disabled={busy || selectedTask.status === 'running' || selectedTask.status === 'waiting'} />
            <div className="composer-footer">
              <span>{terminal.has(selectedTask.status) ? 'Resume session' : selectedTask.status === 'idle' ? 'Ready' : statusLabel[selectedTask.status]}</span>
              <button className="send-button" type="submit" title="Send" disabled={!composer.trim() || busy || selectedTask.status === 'running' || selectedTask.status === 'waiting'}><Play size={15} fill="currentColor" /></button>
            </div>
          </form>
        </> : <div className="main-empty"><Bot size={30} /><span>Select or create a task</span></div>}
      </main>

      {showInspector && <aside className="inspector">
        <div className="inspector-header"><span>Inspector</span><button className="icon-button compact" title="Close inspector" onClick={() => setShowInspector(false)}><X size={16} /></button></div>
        {selectedTask && <>
          <InspectorSection title="Task">
            <InfoRow label="Status" value={statusLabel[selectedTask.status] ?? selectedTask.status} />
            <InfoRow label="PID" value={selectedTask.pid ? String(selectedTask.pid) : '—'} mono />
            <InfoRow label="Events" value={String(selectedTask.lastSequence)} mono />
            <InfoRow label="Resumes" value={String(selectedTask.resumeCount ?? 0)} mono />
            <InfoRow label="Session" value={selectedTask.sessionId ? selectedTask.sessionId.slice(0, 12) : '—'} mono copy={selectedTask.sessionId} />
          </InspectorSection>
          <InspectorSection title="Workspace">
            <div className="workspace-entry"><FolderIcon /><span>{selectedTask.cwd}</span></div>
          </InspectorSection>
        </>}
        {connection && <InspectorSection title="Board">
          <div className="board-identity"><Server size={18} /><div><strong>{connection.board.name}</strong><span>{connection.board.user}@{connection.board.host}</span></div></div>
          <div className="metrics-grid">
            <Metric icon={<Cpu size={15} />} label="Agentd" value={`v${connection.daemon?.version ?? '—'}`} />
            <Metric icon={<Activity size={15} />} label="Tasks" value={`${connection.daemon?.activeTasks ?? 0}/${connection.daemon?.maximumTasks ?? 0}`} />
            <Metric icon={<MemoryStick size={15} />} label="Schema" value={`v${connection.capabilities?.eventSchema ?? '—'}`} />
            <Metric icon={<ShieldCheck size={15} />} label="SSH" value="Verified" />
          </div>
        </InspectorSection>}
      </aside>}

      {error && <div className="error-toast"><XCircle size={17} /><span>{error}</span><button title="Dismiss" onClick={() => setError('')}><X size={15} /></button></div>}
      {showBoard && <BoardDialog boards={boards} busy={busy} onClose={() => boards.length > 0 && setShowBoard(false)} onConnect={connect} onSave={async (board) => {
        setBusy(true);
        setError('');
        try {
          const saved = await api.saveBoard(board);
          setBoards(await api.listBoards());
          await connect(saved);
        } catch (reason) {
          setError(String(reason));
        } finally {
          setBusy(false);
        }
      }} />}
      {showNewTask && connection && <NewTaskDialog busy={busy} onClose={() => setShowNewTask(false)} onCreate={async (request) => { setBusy(true); try { const task = await api.startTask(connection.board.id, request); await refreshTasks(); setSelectedTask(task); setShowNewTask(false); } catch (reason) { setError(String(reason)); } finally { setBusy(false); } }} />}
    </div>
  );
}

function TimelineEvent({event}: {event: TaskEvent}) {
  const normalized = event.normalized;
  if (!normalized) return null;
  const data = normalized.data ?? {};
  const time = new Date(event.time).toLocaleTimeString([], {hour: '2-digit', minute: '2-digit', second: '2-digit'});
  if (normalized.type === 'assistant.thinking.delta') return <details className="thinking-event"><summary><Bot size={15} /> Thinking <span>{time}</span></summary><p>{String(data.delta ?? '')}</p></details>;
  if (normalized.type === 'assistant.text.delta') return <div className="message-event"><div className="event-glyph"><Bot size={16} /></div><div className="message-body"><div className="event-heading"><strong>Hobot Code</strong><span>{time}</span><CopyButton value={String(data.delta ?? '')} /></div><p>{String(data.delta ?? '')}</p></div></div>;
  if (normalized.type === 'tool.started' || normalized.type === 'tool.completed') {
    const complete = normalized.type === 'tool.completed';
    return <div className={`tool-event ${complete ? 'complete' : ''}`}><div className="event-glyph"><SquareTerminal size={15} /></div><div><div className="event-heading"><strong>{String(data.toolName ?? 'Tool')}</strong><span>{time}</span></div><span>{complete ? (data.isError ? 'Failed' : 'Completed') : 'Running'}</span></div>{complete ? data.isError ? <XCircle size={16} /> : <Check size={16} /> : <LoaderCircle size={16} className="spin" />}</div>;
  }
  if (normalized.type === 'approval.requested') return <div className="system-event approval"><ShieldCheck size={15} /><span>Approval requested</span><time>{time}</time></div>;
  if (normalized.type === 'task.running' || normalized.type === 'task.idle') return <div className="system-event"><Activity size={14} /><span>{normalized.type === 'task.running' ? 'Agent started' : 'Agent settled'}</span><time>{time}</time></div>;
  return null;
}

function ApprovalBar({approval, busy, respond}: {approval: Approval; busy: boolean; respond: (response: Record<string, unknown>) => void}) {
  return <div className="approval-bar"><div className="approval-icon"><ShieldCheck size={18} /></div><div className="approval-copy"><strong>{approval.title ?? 'Approval required'}</strong><span>{approval.message}</span></div><div className="approval-actions">
    {approval.method === 'select' ? approval.options?.map((option) => <button key={option} className="secondary-button" disabled={busy} onClick={() => respond({value: option})}>{option}</button>) : <><button className="secondary-button" disabled={busy} onClick={() => respond({confirmed: false})}>Deny</button><button className="primary-button" disabled={busy} onClick={() => respond({confirmed: true})}>Allow</button></>}
  </div></div>;
}

function BoardDialog({boards, busy, onClose, onConnect, onSave}: {boards: Board[]; busy: boolean; onClose: () => void; onConnect: (board: Board) => void; onSave: (board: Board) => Promise<void>}) {
  const [editing, setEditing] = useState(boards.length === 0);
  const [form, setForm] = useState<Board>({id: '', name: 'RDK S100', host: '10.112.10.98', user: 'root', port: 22, identityFile: ''});
  return <div className="modal-backdrop"><div className="modal board-modal">
    <div className="modal-header"><div><span className="modal-eyebrow">Boards</span><h2>{editing ? 'Add board' : 'Connect'}</h2></div>{boards.length > 0 && <button className="icon-button" title="Close" onClick={onClose}><X size={18} /></button>}</div>
    {!editing ? <><div className="saved-boards">{boards.map((board) => <button key={board.id} className="saved-board" onClick={() => onConnect(board)} disabled={busy}><Server size={19} /><span><strong>{board.name}</strong><small>{board.user}@{board.host}:{board.port}</small></span><Play size={15} /></button>)}</div><button className="add-board-row" onClick={() => setEditing(true)}><Plus size={16} /> Add board</button></> :
      <form onSubmit={(event) => {event.preventDefault(); void onSave(form);}} className="form-grid">
        <label><span>Name</span><input value={form.name} onChange={(event) => setForm({...form, name: event.target.value})} required /></label>
        <label><span>Host</span><input value={form.host} onChange={(event) => setForm({...form, host: event.target.value})} required /></label>
        <div className="form-row"><label><span>User</span><input value={form.user} onChange={(event) => setForm({...form, user: event.target.value})} required /></label><label className="port-field"><span>Port</span><input type="number" min="1" max="65535" value={form.port} onChange={(event) => setForm({...form, port: Number(event.target.value)})} required /></label></div>
        <label><span>Identity file</span><input value={form.identityFile} onChange={(event) => setForm({...form, identityFile: event.target.value})} placeholder="Use SSH agent or config" /></label>
        <div className="modal-actions">{boards.length > 0 && <button type="button" className="secondary-button" onClick={() => setEditing(false)}>Back</button>}<button className="primary-button" type="submit" disabled={busy}>{busy ? <LoaderCircle size={15} className="spin" /> : <Server size={15} />} Save & connect</button></div>
      </form>}
  </div></div>;
}

function NewTaskDialog({busy, onClose, onCreate}: {busy: boolean; onClose: () => void; onCreate: (request: {name: string; cwd: string; prompt: string; approve: boolean}) => void}) {
  const [request, setRequest] = useState({name: '', cwd: '/root', prompt: '', approve: false});
  return <div className="modal-backdrop"><form className="modal task-modal" onSubmit={(event) => {event.preventDefault(); onCreate(request);}}><div className="modal-header"><div><span className="modal-eyebrow">Agent task</span><h2>New task</h2></div><button type="button" className="icon-button" title="Close" onClick={onClose}><X size={18} /></button></div><div className="form-grid"><label><span>Name</span><input value={request.name} onChange={(event) => setRequest({...request, name: event.target.value})} placeholder="Optional" /></label><label><span>Workspace</span><input value={request.cwd} onChange={(event) => setRequest({...request, cwd: event.target.value})} required /></label><label><span>Instruction</span><textarea rows={5} value={request.prompt} onChange={(event) => setRequest({...request, prompt: event.target.value})} required /></label><label className="checkbox-row"><input type="checkbox" checked={request.approve} onChange={(event) => setRequest({...request, approve: event.target.checked})} /><span>Trust project resources</span></label><div className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>Cancel</button><button className="primary-button" disabled={busy || !request.prompt.trim()}><Plus size={15} /> Create task</button></div></div></form></div>;
}

function InspectorSection({title, children}: {title: string; children: ReactNode}) { return <section className="inspector-section"><h3>{title}</h3>{children}</section>; }
function InfoRow({label, value, mono, copy}: {label: string; value: string; mono?: boolean; copy?: string}) { return <div className="info-row"><span>{label}</span><div><strong className={mono ? 'mono' : ''}>{value}</strong>{copy && <CopyButton value={copy} />}</div></div>; }
function CopyButton({value}: {value: string}) { return <button type="button" className="copy-button" title="Copy" onClick={() => void navigator.clipboard.writeText(value)}><Clipboard size={13} /></button>; }
function Metric({icon, label, value}: {icon: ReactNode; label: string; value: string}) { return <div className="metric"><span>{icon}{label}</span><strong>{value}</strong></div>; }
function FolderIcon() { return <Box size={16} />; }
function relativeTime(value: string) { const seconds = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 1000)); if (seconds < 60) return 'now'; if (seconds < 3600) return `${Math.floor(seconds / 60)}m`; return `${Math.floor(seconds / 3600)}h`; }

function coalesceEvents(events: TaskEvent[]): TaskEvent[] {
  const result: TaskEvent[] = [];
  for (const event of events) {
    const type = event.normalized?.type;
    const previous = result[result.length - 1];
    if ((type === 'assistant.text.delta' || type === 'assistant.thinking.delta') && previous?.normalized?.type === type) {
      const previousDelta = String(previous.normalized.data?.delta ?? '');
      const nextDelta = String(event.normalized?.data?.delta ?? '');
      result[result.length - 1] = {
        ...event,
        normalized: {...event.normalized!, data: {...event.normalized?.data, delta: previousDelta + nextDelta}},
      };
      continue;
    }
    result.push(event);
  }
  return result;
}

export default App;
