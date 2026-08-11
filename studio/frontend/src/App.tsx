import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import type {FormEvent, ReactNode, UIEvent} from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {
  Activity, ArrowDown, ArrowUp, Bot, Box, Brain, Check, ChevronDown,
  ChevronRight, CircleStop, Clipboard, Cpu, FilePenLine, ListTodo,
  LoaderCircle, MemoryStick, MessageSquare, PanelRight, Plus, RefreshCw,
  Search, Server, ShieldCheck, SquareTerminal, Wrench, X, XCircle,
} from 'lucide-react';
import {api, isMock} from './api';
import {composerIsBlocked, composerMode, shouldSubmitComposer, terminalStatuses} from './composer-policy.js';
import {buildConversation, elapsedLabel} from './conversation-model.js';
import type {AssistantConversationItem, ToolActivity, UserConversationItem} from './conversation-model.js';
import type {Approval, Board, Connection, Task, TaskEvent} from './types';
import './App.css';

const statusLabel: Record<string, string> = {
  starting: 'Starting', idle: 'Ready', running: 'Working', waiting: 'Approval needed',
  stopping: 'Stopping', stopped: 'Stopped', failed: 'Failed', interrupted: 'Interrupted',
};

function App() {
  const [boards, setBoards] = useState<Board[]>([]);
  const [connection, setConnection] = useState<Connection | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  const [events, setEvents] = useState<TaskEvent[]>([]);
  const [search, setSearch] = useState('');
  const [composer, setComposer] = useState('');
  const [editingMessage, setEditingMessage] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [eventsLoading, setEventsLoading] = useState(false);
  const [error, setError] = useState('');
  const [showBoard, setShowBoard] = useState(false);
  const [showNewTask, setShowNewTask] = useState(false);
  const [showInspector, setShowInspector] = useState(false);
  const [watchRevision, setWatchRevision] = useState(0);
  const [hasNewOutput, setHasNewOutput] = useState(false);
  const startupStarted = useRef(false);
  const timelineRef = useRef<HTMLDivElement>(null);
  const composerRef = useRef<HTMLTextAreaElement>(null);
  const followsOutput = useRef(true);
  const taskDrafts = useRef(new Map<string, {text: string; editingMessage: number | null}>());
  const previousTaskId = useRef('');

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
      if (['task.running', 'task.idle', 'approval.requested'].includes(event.normalized?.type ?? '')) void refreshTasks();
    });
    const removeError = api.onWatchError((watchError) => {
      if (watchError.boardId === boardId) setError(watchError.error);
    });
    return () => { removeEvent(); removeError(); };
  }, [boardId, refreshTasks, selectedTask?.id]);

  useEffect(() => {
    followsOutput.current = true;
    setHasNewOutput(false);
    if (!boardId || !selectedTask) {
      setEvents([]);
      return;
    }
    let active = true;
    setEventsLoading(true);
    api.events(boardId, selectedTask.id).then((page) => {
      if (!active) return;
      setEvents(page.events ?? []);
      const after = page.nextAfter ?? page.events[page.events.length - 1]?.sequence ?? 0;
      return api.watch(boardId, selectedTask.id, after);
    }).catch((reason) => setError(String(reason))).finally(() => {
      if (active) setEventsLoading(false);
    });
    return () => {
      active = false;
      void api.stopWatch(boardId, selectedTask.id);
    };
  }, [boardId, selectedTask?.id, watchRevision]);

  useEffect(() => {
    const nextTaskId = selectedTask?.id ?? '';
    const previous = previousTaskId.current;
    if (previous && previous !== nextTaskId) {
      if (composer) taskDrafts.current.set(previous, {text: composer, editingMessage});
      else taskDrafts.current.delete(previous);
    }
    if (previous !== nextTaskId) {
      const draft = taskDrafts.current.get(nextTaskId);
      setComposer(draft?.text ?? '');
      setEditingMessage(draft?.editingMessage ?? null);
      previousTaskId.current = nextTaskId;
    }
  }, [selectedTask?.id]);

  useEffect(() => {
    if (!boardId) return;
    const timer = window.setInterval(() => void refreshTasks(), 5000);
    return () => window.clearInterval(timer);
  }, [boardId, refreshTasks]);

  useEffect(() => {
    const timeline = timelineRef.current;
    if (!timeline) return;
    if (followsOutput.current) {
      window.requestAnimationFrame(() => timeline.scrollTo({top: timeline.scrollHeight, behavior: 'smooth'}));
      setHasNewOutput(false);
    } else if (events.length > 0) {
      setHasNewOutput(true);
    }
  }, [events.length, selectedTask?.status]);

  useEffect(() => {
    const textarea = composerRef.current;
    if (!textarea) return;
    textarea.style.height = 'auto';
    textarea.style.height = `${Math.min(180, Math.max(54, textarea.scrollHeight))}px`;
  }, [composer]);

  const visibleTasks = useMemo(() => {
    const query = search.trim().toLowerCase();
    return query ? tasks.filter((task) => `${task.name} ${task.cwd} ${task.status}`.toLowerCase().includes(query)) : tasks;
  }, [search, tasks]);
  const conversation = useMemo(() => buildConversation(events), [events]);
  const activeApproval = selectedTask?.pendingApprovals?.find((approval) => approval.active);
  const selectedComposerMode = selectedTask ? composerMode(selectedTask) : 'send';
  const composerBlocked = busy || (selectedTask ? composerIsBlocked(selectedTask.status) : true);
  const activeTaskCount = tasks.filter((task) => !terminalStatuses.has(task.status)).length;

  async function submitPrompt(event: FormEvent) {
    event.preventDefault();
    const prompt = composer.trim();
    if (!prompt || !selectedTask || !boardId || composerBlocked) return;
    setBusy(true);
    setError('');
    try {
      let nextTask: Task | undefined;
      if (selectedComposerMode === 'resume') nextTask = await api.resumeTask(boardId, selectedTask.id, prompt);
      else if (selectedComposerMode === 'restart') nextTask = await api.restartTask(boardId, selectedTask.id, prompt);
      else await api.sendPrompt(boardId, selectedTask.id, prompt);
      setComposer('');
      setEditingMessage(null);
      taskDrafts.current.delete(selectedTask.id);
      followsOutput.current = true;
      await refreshTasks();
      if (nextTask) {
        setSelectedTask(nextTask);
        setWatchRevision((revision) => revision + 1);
      }
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  async function stopTask() {
    if (!selectedTask || !boardId) return;
    setBusy(true);
    setError('');
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
    setError('');
    try {
      await api.respond(boardId, selectedTask.id, approval.id, response);
      await refreshTasks();
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  function editMessage(item: UserConversationItem) {
    setComposer(item.text);
    setEditingMessage(item.sequence);
    window.requestAnimationFrame(() => {
      composerRef.current?.focus();
      composerRef.current?.setSelectionRange(item.text.length, item.text.length);
    });
  }

  function onTimelineScroll(event: UIEvent<HTMLDivElement>) {
    const target = event.currentTarget;
    const atBottom = target.scrollHeight - target.scrollTop - target.clientHeight < 80;
    followsOutput.current = atBottom;
    if (atBottom) setHasNewOutput(false);
  }

  function scrollToLatest() {
    followsOutput.current = true;
    setHasNewOutput(false);
    timelineRef.current?.scrollTo({top: timelineRef.current.scrollHeight, behavior: 'smooth'});
  }

  return (
    <div className={`studio-shell ${showInspector ? '' : 'inspector-hidden'}`}>
      <header className="titlebar">
        <div className="brand-lockup"><div className="brand-mark" aria-label="Hobot Code">H</div><span>Hobot Code</span></div>
        <button className="board-switcher" onClick={() => setShowBoard(true)} disabled={busy}>
          <span className={`connection-dot ${connection ? 'online' : ''}`} />
          <span className="board-name">{connection?.board.name ?? 'Connect board'}</span>
          <ChevronDown size={14} />
        </button>
        <div className="titlebar-spacer" />
        {isMock() && <span className="preview-label">Preview</span>}
        <button className="icon-button" title="Refresh tasks" onClick={() => void refreshTasks()}><RefreshCw size={16} /></button>
        <button className={`icon-button ${showInspector ? 'active' : ''}`} title="Task details" onClick={() => setShowInspector((value) => !value)}><PanelRight size={17} /></button>
      </header>

      <aside className="task-sidebar">
        <div className="sidebar-heading">
          <div><span className="section-label">Tasks</span><span className="task-count">{tasks.length}</span></div>
          <button className="icon-button compact" title="New task" onClick={() => setShowNewTask(true)} disabled={!connection}><Plus size={17} /></button>
        </div>
        <label className="search-field"><Search size={15} /><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search tasks" /></label>
        <div className="task-list">
          {visibleTasks.map((task) => (
            <button key={task.id} className={`task-row ${selectedTask?.id === task.id ? 'selected' : ''}`} onClick={() => setSelectedTask(task)}>
              <span className={`task-state-dot dot-${task.status}`} />
              <span className="task-row-main"><span className="task-row-name">{task.name}</span><span className="task-row-path">{task.cwd}</span></span>
              <span className="task-row-time">{relativeTime(task.updatedAt)}</span>
            </button>
          ))}
          {visibleTasks.length === 0 && <div className="empty-state"><ListTodo size={21} /><span>No tasks</span></div>}
        </div>
        {connection && <button className="board-summary" onClick={() => setShowBoard(true)}>
          <Server size={16} /><span><strong>{connection.board.name}</strong><small>{activeTaskCount}/{connection.daemon?.maximumTasks ?? 0} active</small></span><ChevronRight size={15} />
        </button>}
      </aside>

      <main className="task-main">
        {selectedTask ? <>
          <div className="task-header">
            <div className="task-title-block"><div className="task-title-line"><h1>{selectedTask.name}</h1><span className={`status status-${selectedTask.status}`}>{statusLabel[selectedTask.status] ?? selectedTask.status}</span></div><span className="workspace-path">{selectedTask.cwd}</span></div>
            <div className="task-actions">
              {terminalStatuses.has(selectedTask.status)
                ? <button className="secondary-button" onClick={() => composerRef.current?.focus()}><RefreshCw size={14} />{selectedComposerMode === 'resume' ? 'Resume' : 'New session'}</button>
                : <button className="secondary-button" onClick={stopTask} disabled={busy || selectedTask.status === 'stopping'}><CircleStop size={14} />Stop</button>}
            </div>
          </div>

          <div className="conversation" ref={timelineRef} onScroll={onTimelineScroll} aria-label="Conversation">
            <div className="conversation-inner">
              {eventsLoading && events.length === 0 && <div className="loading-conversation"><LoaderCircle size={18} className="spin" /><span>Loading conversation</span></div>}
              {!eventsLoading && conversation.length === 0 && <div className="empty-conversation"><div className="empty-symbol"><MessageSquare size={22} /></div><strong>Start a conversation</strong><span>Ask Hobot Code to inspect, build, debug, or deploy on this board.</span></div>}
              {conversation.map((item) => item.kind === 'user'
                ? <UserMessage key={item.key} item={item} onEdit={editMessage} />
                : <AssistantTurn key={item.key} item={item} running={selectedTask.status === 'running' && item === conversation[conversation.length - 1]} />)}
              {selectedTask.status === 'starting' && <div className="agent-progress"><LoaderCircle size={15} className="spin" /><span>Starting agent</span></div>}
            </div>
          </div>

          {hasNewOutput && <button className="jump-latest" onClick={scrollToLatest}><ArrowDown size={15} />New output</button>}
          <div className="composer-dock">
            {activeApproval && <ApprovalBar approval={activeApproval} busy={busy} respond={(response) => respond(activeApproval, response)} />}
            <form className="composer" onSubmit={submitPrompt}>
              {editingMessage !== null && <div className="editing-banner"><FilePenLine size={14} /><span>Edited prompt</span><button type="button" title="Cancel edit" onClick={() => {setEditingMessage(null); setComposer('');}}><X size={14} /></button></div>}
              <textarea
                ref={composerRef}
                id="composer"
                value={composer}
                onChange={(event) => setComposer(event.target.value)}
                onKeyDown={(event) => {
                  const isComposing = event.nativeEvent.isComposing || event.keyCode === 229;
                  if (!shouldSubmitComposer(event.key, event.shiftKey, isComposing)) return;
                  event.preventDefault();
                  if (!composerBlocked && composer.trim()) event.currentTarget.form?.requestSubmit();
                }}
                placeholder={selectedComposerMode === 'resume' ? 'Continue this task' : selectedComposerMode === 'restart' ? 'Start a new session' : 'Message Hobot Code'}
                rows={2}
              />
              <div className="composer-footer">
                <span>{selectedComposerMode === 'resume' ? 'Resume session' : selectedComposerMode === 'restart' ? 'New session' : statusLabel[selectedTask.status] ?? selectedTask.status}</span>
                {['running', 'starting'].includes(selectedTask.status) && <button className="composer-stop" type="button" title="Stop agent" onClick={stopTask} disabled={busy}><SquareTerminal size={14} /></button>}
                <button className="send-button" type="submit" title="Send" disabled={!composer.trim() || composerBlocked}><ArrowUp size={17} /></button>
              </div>
            </form>
          </div>
        </> : <div className="main-empty"><div className="empty-symbol"><Bot size={24} /></div><strong>Select a task</strong><span>Choose an existing conversation or create a new one.</span></div>}
      </main>

      {showInspector && <aside className="inspector">
        <div className="inspector-header"><span>Details</span><button className="icon-button compact" title="Close details" onClick={() => setShowInspector(false)}><X size={16} /></button></div>
        {selectedTask && <><InspectorSection title="Task"><InfoRow label="Status" value={statusLabel[selectedTask.status] ?? selectedTask.status} /><InfoRow label="PID" value={selectedTask.pid ? String(selectedTask.pid) : '—'} mono /><InfoRow label="Events" value={String(selectedTask.lastSequence)} mono /><InfoRow label="Resumes" value={String(selectedTask.resumeCount ?? 0)} mono /><InfoRow label="Restarts" value={String(selectedTask.restartCount ?? 0)} mono /><InfoRow label="Session" value={selectedTask.sessionId ? selectedTask.sessionId.slice(0, 12) : '—'} mono copy={selectedTask.sessionId} /></InspectorSection><InspectorSection title="Workspace"><div className="workspace-entry"><Box size={16} /><span>{selectedTask.cwd}</span></div></InspectorSection></>}
        {connection && <InspectorSection title="Board"><div className="board-identity"><Server size={18} /><div><strong>{connection.board.name}</strong><span>{connection.board.user}@{connection.board.host}</span></div></div><div className="metrics-grid"><Metric icon={<Cpu size={15} />} label="Agentd" value={`v${connection.daemon?.version ?? '—'}`} /><Metric icon={<Activity size={15} />} label="Tasks" value={`${activeTaskCount}/${connection.daemon?.maximumTasks ?? 0}`} /><Metric icon={<MemoryStick size={15} />} label="Schema" value={`v${connection.capabilities?.eventSchema ?? '—'}`} /><Metric icon={<ShieldCheck size={15} />} label="SSH" value="Verified" /></div></InspectorSection>}
      </aside>}

      {error && <div className="error-toast"><XCircle size={17} /><span>{friendlyError(error)}</span><button title="Dismiss" onClick={() => setError('')}><X size={15} /></button></div>}
      {showBoard && <BoardDialog boards={boards} busy={busy} onClose={() => boards.length > 0 && setShowBoard(false)} onConnect={connect} onSave={async (board) => {setBusy(true); setError(''); try {const saved = await api.saveBoard(board); setBoards(await api.listBoards()); await connect(saved);} catch (reason) {setError(String(reason));} finally {setBusy(false);}}} />}
      {showNewTask && connection && <NewTaskDialog busy={busy} onClose={() => setShowNewTask(false)} onCreate={async (request) => {setBusy(true); setError(''); try {const task = await api.startTask(connection.board.id, request); await refreshTasks(); setSelectedTask(task); setShowNewTask(false);} catch (reason) {setError(String(reason));} finally {setBusy(false);}}} />}
    </div>
  );
}

function UserMessage({item, onEdit}: {item: UserConversationItem; onEdit: (item: UserConversationItem) => void}) {
  return <article className="user-message"><div className="message-meta"><strong>You</strong><time>{formatTime(item.time)}</time><span className="message-actions"><CopyButton value={item.text} /><button className="copy-button" title="Edit and send again" onClick={() => onEdit(item)}><FilePenLine size={14} /></button></span></div><div className="user-message-content">{item.text}</div></article>;
}

function AssistantTurn({item, running}: {item: AssistantConversationItem; running: boolean}) {
  return <article className="assistant-turn">
    <div className="assistant-heading"><div className="assistant-avatar">H</div><strong>Hobot Code</strong><time>{formatTime(item.startedAt)}</time></div>
    {item.thinking && <ThinkingBlock item={item} running={running} />}
    {item.tools.length > 0 && <ToolGroup tools={item.tools} />}
    {item.notices.map((notice, index) => <div key={`${notice.time}-${index}`} className={`turn-notice notice-${notice.type}`}><Activity size={13} /><span>{notice.label}</span></div>)}
    {item.text && <div className="assistant-content"><MarkdownContent value={item.text} /><div className="assistant-actions"><CopyButton value={item.text} /></div></div>}
    {running && <div className="agent-progress"><LoaderCircle size={14} className="spin" /><span>Working</span></div>}
  </article>;
}

function ThinkingBlock({item, running}: {item: AssistantConversationItem; running: boolean}) {
  const [open, setOpen] = useState(running && !item.text);
  return <details className="thinking-block" open={open} onToggle={(event) => setOpen(event.currentTarget.open)}><summary><Brain size={15} /><span>{running ? 'Thinking' : `Thought for ${elapsedLabel(item.startedAt, item.endedAt)}`}</span><ChevronRight className="details-chevron" size={14} /></summary><div className="thinking-content"><MarkdownContent value={item.thinking} /></div></details>;
}

function ToolGroup({tools}: {tools: ToolActivity[]}) {
  const failed = tools.some((tool) => tool.isError);
  const active = tools.some((tool) => tool.status === 'running');
  const label = tools.length === 1 ? `${active ? 'Running' : 'Ran'} ${tools[0].name}` : `${active ? 'Using' : 'Used'} ${tools.length} tools`;
  const [open, setOpen] = useState(failed);
  return <details className={`tool-group ${failed ? 'failed' : ''}`} open={open} onToggle={(event) => setOpen(event.currentTarget.open)}>
    <summary><Wrench size={15} /><span>{label}</span><small>{active ? 'In progress' : failed ? 'Completed with errors' : 'Completed'}</small><ChevronRight className="details-chevron" size={14} /></summary>
    <div className="tool-list">{tools.map((tool) => <ToolRow key={tool.id} tool={tool} />)}</div>
  </details>;
}

function ToolRow({tool}: {tool: ToolActivity}) {
  const hasDetail = Boolean(tool.input || tool.output);
  const content = <><span className={`tool-status ${tool.status === 'running' ? 'running' : tool.isError ? 'error' : ''}`}>{tool.status === 'running' ? <LoaderCircle size={13} className="spin" /> : tool.isError ? <XCircle size={13} /> : <Check size={13} />}</span><SquareTerminal size={14} /><strong>{tool.name}</strong><small>{elapsedLabel(tool.startedAt, tool.endedAt)}</small>{hasDetail && <ChevronRight className="details-chevron" size={13} />}</>;
  if (!hasDetail) return <div className="tool-row">{content}</div>;
  return <details className="tool-row expandable"><summary>{content}</summary><div className="tool-detail">{tool.input && <><span>Input</span><pre>{tool.input}</pre></>}{tool.output && <><span>Output</span><pre>{tool.output}</pre></>}</div></details>;
}

function MarkdownContent({value}: {value: string}) {
  return <div className="markdown"><ReactMarkdown skipHtml remarkPlugins={[remarkGfm]} components={{
    a: ({node: _node, ...props}: any) => <a {...props} target="_blank" rel="noreferrer" />,
  }}>{value}</ReactMarkdown></div>;
}

function ApprovalBar({approval, busy, respond}: {approval: Approval; busy: boolean; respond: (response: Record<string, unknown>) => void}) {
  return <div className="approval-bar"><div className="approval-icon"><ShieldCheck size={17} /></div><div className="approval-copy"><strong>{approval.title ?? 'Approval required'}</strong><span>{approval.message}</span></div><div className="approval-actions">{approval.method === 'select' ? approval.options?.map((option) => <button key={option} className="secondary-button" disabled={busy} onClick={() => respond({value: option})}>{option}</button>) : <><button className="secondary-button" disabled={busy} onClick={() => respond({confirmed: false})}>Deny</button><button className="primary-button" disabled={busy} onClick={() => respond({confirmed: true})}>Allow</button></>}</div></div>;
}

function BoardDialog({boards, busy, onClose, onConnect, onSave}: {boards: Board[]; busy: boolean; onClose: () => void; onConnect: (board: Board) => void; onSave: (board: Board) => Promise<void>}) {
  const [editing, setEditing] = useState(boards.length === 0);
  const [form, setForm] = useState<Board>({id: '', name: 'RDK S100', host: '10.112.10.98', user: 'root', port: 22, identityFile: ''});
  return <div className="modal-backdrop"><div className="modal board-modal"><div className="modal-header"><div><span className="modal-eyebrow">Boards</span><h2>{editing ? 'Add board' : 'Connect'}</h2></div>{boards.length > 0 && <button className="icon-button" title="Close" onClick={onClose}><X size={18} /></button>}</div>{!editing ? <><div className="saved-boards">{boards.map((board) => <button key={board.id} className="saved-board" onClick={() => onConnect(board)} disabled={busy}><Server size={19} /><span><strong>{board.name}</strong><small>{board.user}@{board.host}:{board.port}</small></span><ChevronRight size={15} /></button>)}</div><button className="add-board-row" onClick={() => setEditing(true)}><Plus size={16} />Add board</button></> : <form onSubmit={(event) => {event.preventDefault(); void onSave(form);}} className="form-grid"><label><span>Name</span><input value={form.name} onChange={(event) => setForm({...form, name: event.target.value})} required /></label><label><span>Host</span><input value={form.host} onChange={(event) => setForm({...form, host: event.target.value})} required /></label><div className="form-row"><label><span>User</span><input value={form.user} onChange={(event) => setForm({...form, user: event.target.value})} required /></label><label><span>Port</span><input type="number" min="1" max="65535" value={form.port} onChange={(event) => setForm({...form, port: Number(event.target.value)})} required /></label></div><label><span>Identity file</span><input value={form.identityFile} onChange={(event) => setForm({...form, identityFile: event.target.value})} placeholder="Use SSH agent or config" /></label><div className="modal-actions">{boards.length > 0 && <button type="button" className="secondary-button" onClick={() => setEditing(false)}>Back</button>}<button className="primary-button" type="submit" disabled={busy}>{busy ? <LoaderCircle size={15} className="spin" /> : <Server size={15} />}Save & connect</button></div></form>}</div></div>;
}

function NewTaskDialog({busy, onClose, onCreate}: {busy: boolean; onClose: () => void; onCreate: (request: {name: string; cwd: string; prompt: string; approve: boolean}) => void}) {
  const [request, setRequest] = useState({name: '', cwd: '/root', prompt: '', approve: false});
  return <div className="modal-backdrop"><form className="modal task-modal" onSubmit={(event) => {event.preventDefault(); onCreate(request);}}><div className="modal-header"><div><span className="modal-eyebrow">Agent task</span><h2>New task</h2></div><button type="button" className="icon-button" title="Close" onClick={onClose}><X size={18} /></button></div><div className="form-grid"><label><span>Name</span><input value={request.name} onChange={(event) => setRequest({...request, name: event.target.value})} placeholder="Optional" /></label><label><span>Workspace</span><input value={request.cwd} onChange={(event) => setRequest({...request, cwd: event.target.value})} required /></label><label><span>Instruction</span><textarea rows={5} value={request.prompt} onChange={(event) => setRequest({...request, prompt: event.target.value})} required /></label><label className="checkbox-row"><input type="checkbox" checked={request.approve} onChange={(event) => setRequest({...request, approve: event.target.checked})} /><span>Trust project resources</span></label><div className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>Cancel</button><button className="primary-button" disabled={busy || !request.prompt.trim()}><Plus size={15} />Create task</button></div></div></form></div>;
}

function InspectorSection({title, children}: {title: string; children: ReactNode}) { return <section className="inspector-section"><h3>{title}</h3>{children}</section>; }
function InfoRow({label, value, mono, copy}: {label: string; value: string; mono?: boolean; copy?: string}) { return <div className="info-row"><span>{label}</span><div><strong className={mono ? 'mono' : ''}>{value}</strong>{copy && <CopyButton value={copy} />}</div></div>; }
function CopyButton({value}: {value: string}) { const [copied, setCopied] = useState(false); return <button type="button" className="copy-button" title={copied ? 'Copied' : 'Copy'} onClick={() => void navigator.clipboard.writeText(value).then(() => {setCopied(true); window.setTimeout(() => setCopied(false), 1200);})}>{copied ? <Check size={13} /> : <Clipboard size={13} />}</button>; }
function Metric({icon, label, value}: {icon: ReactNode; label: string; value: string}) { return <div className="metric"><span>{icon}{label}</span><strong>{value}</strong></div>; }
function formatTime(value: string) { return new Date(value).toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'}); }
function relativeTime(value: string) { const seconds = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 1000)); if (seconds < 60) return 'now'; if (seconds < 3600) return `${Math.floor(seconds / 60)}m`; if (seconds < 86_400) return `${Math.floor(seconds / 3600)}h`; return `${Math.floor(seconds / 86_400)}d`; }
function friendlyError(value: string) {
  const message = value.replace(/^Error:\s*/i, '').replace(/^task_[a-z_]+:\s*/i, '');
  if (/context deadline exceeded|operation timed out|connect to host .* timed out/i.test(message)) return 'Could not reach the board. Check the network or VPN and try again.';
  if (/requires a newer Hobot Code event schema/i.test(message)) return 'Update the board-side Hobot Code and reconnect.';
  if (/has no resumable Hobot Code session/i.test(message)) return 'This task has no saved session. Start a new session instead.';
  return message;
}

export default App;
