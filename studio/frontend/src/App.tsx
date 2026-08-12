import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import type {FormEvent, ReactNode, UIEvent} from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {
  Activity, ArrowDown, ArrowLeft, ArrowUp, Bot, Box, Brain, Check, ChevronDown,
  ChevronRight, Clipboard, CornerDownRight, Cpu, FilePenLine, Folder,
  FolderOpen, GitBranch, ListTodo, LoaderCircle, MemoryStick, MessageSquare,
  MoreHorizontal, PanelRight, Paperclip, Plus, RefreshCw, Search, Server, ShieldCheck,
  Square, SquareTerminal, Trash2, Wrench, X, XCircle,
} from 'lucide-react';
import {api, isMock} from './api';
import {composerIsBlocked, composerMode, shouldSubmitComposer, terminalStatuses} from './composer-policy.js';
import {buildConversation, elapsedLabel, recentEventsAfter} from './conversation-model.js';
import {approvalPresentation} from './approval-model.js';
import {arrangeTasks, groupTasksByProject} from './project-model.js';
import type {AssistantConversationItem, ToolActivity, UserConversationItem} from './conversation-model.js';
import type {Approval, Board, Connection, ImageContent, ModelOption, Task, TaskEvent, WorkspaceListing} from './types';
import './App.css';

const isMacOS = typeof navigator !== 'undefined' && /Mac/.test(navigator.platform);

const statusLabel: Record<string, string> = {
  starting: 'Starting', idle: 'Ready', running: 'Working', waiting: 'Approval needed',
  stopping: 'Stopping', stopped: 'Stopped', failed: 'Failed', interrupted: 'Interrupted',
};

const boardPresets: Array<Omit<Board, 'id'>> = [
  {name: 'RDK S100', host: '10.112.10.98', user: 'root', port: 22},
  {name: 'RDK S600', host: '10.112.10.106', user: 'root', port: 22},
  {name: 'RDK X5', host: '10.112.10.100', user: 'root', port: 22},
];

function App() {
  const [boards, setBoards] = useState<Board[]>([]);
  const [connection, setConnection] = useState<Connection | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  const [events, setEvents] = useState<TaskEvent[]>([]);
  const [search, setSearch] = useState('');
  const [composer, setComposer] = useState('');
  const [editingMessage, setEditingMessage] = useState<number | null>(null);
  const [models, setModels] = useState<ModelOption[]>([]);
  const [attachments, setAttachments] = useState<ImageContent[]>([]);
  const [optimisticPrompt, setOptimisticPrompt] = useState<{taskId: string; text: string; time: string; attachments: ImageContent[]} | null>(null);
  const [showSideTask, setShowSideTask] = useState(false);
  const [activityClock, setActivityClock] = useState(Date.now());
  const [busy, setBusy] = useState(false);
  const [eventsLoading, setEventsLoading] = useState(false);
  const [error, setError] = useState('');
  const [showBoard, setShowBoard] = useState(false);
  const [showNewTask, setShowNewTask] = useState(false);
  const [newTaskProject, setNewTaskProject] = useState('');
  const [showInspector, setShowInspector] = useState(false);
  const [collapsedProjects, setCollapsedProjects] = useState<Set<string>>(new Set());
  const [openMenu, setOpenMenu] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<{kind: 'conversation' | 'project'; label: string; taskIds: string[]} | null>(null);
  const [renamingTask, setRenamingTask] = useState(false);
  const [renameValue, setRenameValue] = useState('');
  const [watchRevision, setWatchRevision] = useState(0);
  const [hasNewOutput, setHasNewOutput] = useState(false);
  const startupStarted = useRef(false);
  const timelineRef = useRef<HTMLDivElement>(null);
  const composerRef = useRef<HTMLTextAreaElement>(null);
  const followsOutput = useRef(true);
  const taskDrafts = useRef(new Map<string, {text: string; editingMessage: number | null; attachments: ImageContent[]}>());
  const previousTaskId = useRef('');
  const activeBoardId = useRef('');

  const boardId = connection?.board.id ?? '';

  const refreshTasks = useCallback(async (targetBoard = boardId) => {
    if (!targetBoard) return;
    try {
      const page = await api.tasks(targetBoard);
      if (targetBoard !== activeBoardId.current) return;
      setTasks(page.tasks ?? []);
      const summary = page.tasks?.find((task) => task.id === selectedTask?.id) ?? page.tasks?.[0] ?? null;
      setSelectedTask(summary);
      if (summary) {
        const detail = await api.task(targetBoard, summary.id);
        if (targetBoard === activeBoardId.current) setSelectedTask(detail);
      }
    } catch (reason) {
      setError(String(reason));
    }
  }, [boardId, selectedTask?.id]);

  const connect = useCallback(async (board: Board) => {
    setBusy(true);
    setError('');
    try {
      const previousBoard = activeBoardId.current;
      const next = await api.connectBoard(board.id);
      const [pageModels, page] = await Promise.all([
        api.models(board.id).catch(() => []),
        api.tasks(board.id),
      ]);
      activeBoardId.current = board.id;
      if (previousBoard && previousBoard !== board.id) await api.disconnectBoard(previousBoard).catch(() => undefined);
      setConnection(next);
      setModels(pageModels ?? []);
      setTasks(page.tasks ?? []);
      const initialTask = page.tasks?.[0] ?? null;
      setSelectedTask(initialTask);
      if (initialTask) setSelectedTask(await api.task(board.id, initialTask.id));
      setEvents([]);
      setOptimisticPrompt(null);
      setError('');
      setShowBoard(false);
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  }, []);

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
    const closeMenus = (event: PointerEvent) => {
      const target = event.target;
      if (target instanceof Element && target.closest('.row-more, .row-menu')) return;
      setOpenMenu('');
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpenMenu('');
    };
    document.addEventListener('pointerdown', closeMenus);
    document.addEventListener('keydown', closeOnEscape);
    return () => {
      document.removeEventListener('pointerdown', closeMenus);
      document.removeEventListener('keydown', closeOnEscape);
    };
  }, []);

  useEffect(() => {
    const removeEvent = api.onEvent(({boardId: eventBoard, event}) => {
      if (eventBoard !== activeBoardId.current || event.taskId !== selectedTask?.id) return;
      setEvents((current) => current.some((item) => item.sequence === event.sequence) ? current : [...current, event]);
      if (['task.running', 'task.idle', 'approval.requested'].includes(event.normalized?.type ?? '')) void refreshTasks();
    });
    const removeError = api.onWatchError((watchError) => {
      if (watchError.boardId === activeBoardId.current) setError(watchError.error);
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
    const initialAfter = recentEventsAfter(selectedTask.lastSequence);
    api.events(boardId, selectedTask.id, initialAfter, 400).then((page) => {
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
      if (composer || attachments.length) taskDrafts.current.set(previous, {text: composer, editingMessage, attachments});
      else taskDrafts.current.delete(previous);
    }
    if (previous !== nextTaskId) {
      const draft = taskDrafts.current.get(nextTaskId);
      setComposer(draft?.text ?? '');
      setEditingMessage(draft?.editingMessage ?? null);
      setAttachments(draft?.attachments ?? []);
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
    if (!selectedTask || !['starting', 'running'].includes(selectedTask.status)) return;
    setActivityClock(Date.now());
    const timer = window.setInterval(() => setActivityClock(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [selectedTask?.id, selectedTask?.status]);

  useEffect(() => {
    if (!optimisticPrompt || optimisticPrompt.taskId !== selectedTask?.id) return;
    const persisted = events.some((event) => event.normalized?.type === 'user.message' && String(event.normalized.data?.text ?? '') === optimisticPrompt.text);
    if (persisted) setOptimisticPrompt(null);
  }, [events, optimisticPrompt, selectedTask?.id]);

  useEffect(() => {
    const textarea = composerRef.current;
    if (!textarea) return;
    textarea.style.height = 'auto';
    textarea.style.height = `${Math.min(180, Math.max(44, textarea.scrollHeight))}px`;
  }, [composer]);

  const visibleTasks = useMemo(() => {
    const query = search.trim().toLowerCase();
    return query ? tasks.filter((task) => `${task.name} ${task.cwd} ${task.status}`.toLowerCase().includes(query)) : tasks;
  }, [search, tasks]);
  const conversation = useMemo(() => buildConversation(events), [events]);
  const projects = useMemo(() => groupTasksByProject(visibleTasks), [visibleTasks]);
  const activeApproval = selectedTask?.pendingApprovals?.find((approval) => approval.active);
  const selectedComposerMode = selectedTask ? composerMode(selectedTask) : 'send';
  const composerBlocked = busy || (selectedTask ? composerIsBlocked(selectedTask.status) : true);
  const activeTaskCount = tasks.filter((task) => !terminalStatuses.has(task.status)).length;
  const selectedModel = selectedTask?.model ?? '';
  const modelPickerValue = selectedModel.startsWith('drobotics/') ? selectedModel : '';
  const selectedPermissionMode = selectedTask?.permissionMode ?? 'ask';
  const canChangeModel = Boolean(selectedTask && (selectedTask.status === 'idle' || terminalStatuses.has(selectedTask.status)) && !busy);
  const canChangePermissions = canChangeModel;
  const canStopTask = Boolean(selectedTask && ['starting', 'running', 'waiting', 'stopping'].includes(selectedTask.status));
  const latestConversationItem = conversation[conversation.length - 1];
  const activityStart = optimisticPrompt && optimisticPrompt.taskId === selectedTask?.id
    ? optimisticPrompt.time
    : latestConversationItem?.kind === 'user' ? latestConversationItem.time : selectedTask?.updatedAt;

  async function submitPrompt(event: FormEvent) {
    event.preventDefault();
    const prompt = composer.trim();
    if (!prompt || !selectedTask || !boardId || composerBlocked) return;
    setBusy(true);
    setError('');
    const submittedAt = new Date().toISOString();
    const submittedImages = attachments;
    setOptimisticPrompt({taskId: selectedTask.id, text: prompt, time: submittedAt, attachments: submittedImages});
    setSelectedTask((current) => current?.id === selectedTask.id ? {...current, status: 'running', updatedAt: submittedAt} : current);
    setComposer('');
    setAttachments([]);
    followsOutput.current = true;
    try {
      let nextTask: Task | undefined;
      if (editingMessage !== null) nextTask = await api.forkTask(boardId, {taskId: selectedTask.id, sequence: editingMessage, prompt, images: submittedImages, kind: 'edit', model: selectedModel});
      else if (selectedComposerMode === 'resume') nextTask = await api.resumeTask(boardId, selectedTask.id, prompt, submittedImages);
      else if (selectedComposerMode === 'restart') nextTask = await api.restartTask(boardId, selectedTask.id, prompt, submittedImages);
      else await api.sendPrompt(boardId, selectedTask.id, prompt, submittedImages);
      setEditingMessage(null);
      taskDrafts.current.delete(selectedTask.id);
      await refreshTasks();
      if (nextTask) {
        setOptimisticPrompt({taskId: nextTask.id, text: prompt, time: submittedAt, attachments: submittedImages});
        setSelectedTask(nextTask);
        setWatchRevision((revision) => revision + 1);
      }
    } catch (reason) {
      setComposer(prompt);
      setAttachments(submittedImages);
      setOptimisticPrompt(null);
      setSelectedTask(selectedTask);
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
    setAttachments([]);
    setEditingMessage(item.sequence);
    window.requestAnimationFrame(() => {
      composerRef.current?.focus();
      composerRef.current?.setSelectionRange(item.text.length, item.text.length);
    });
  }

  async function changeModel(value: string) {
    if (!selectedTask || !boardId || (selectedTask.status !== 'idle' && !terminalStatuses.has(selectedTask.status))) return;
    const [provider, ...rest] = value.split('/');
    const modelId = rest.join('/');
    if (!provider || !modelId) return;
    setBusy(true);
    setError('');
    try {
      await api.setModel(boardId, selectedTask.id, provider, modelId);
      setSelectedTask({...selectedTask, model: value});
      setTasks((current) => current.map((task) => task.id === selectedTask.id ? {...task, model: value} : task));
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  async function changePermissionMode(mode: string) {
    if (!selectedTask || !boardId || !canChangePermissions) return;
    setBusy(true);
    setError('');
    try {
      const task = await api.setPermissionMode(boardId, selectedTask.id, mode);
      setSelectedTask(task);
      setTasks((current) => current.map((item) => item.id === task.id ? task : item));
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  async function renameSelectedTask() {
    const name = renameValue.trim();
    if (!selectedTask || !boardId || !name || name === selectedTask.name) {
      setRenamingTask(false);
      return;
    }
    setBusy(true);
    setError('');
    try {
      const task = await api.renameTask(boardId, selectedTask.id, name);
      setSelectedTask(task);
      setTasks((current) => current.map((item) => item.id === task.id ? task : item));
      setRenamingTask(false);
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  function openNewTask(cwd = '') {
    setNewTaskProject(cwd);
    setShowNewTask(true);
  }

  async function createSideTask(prompt: string, name: string) {
    if (!selectedTask || !boardId) return;
    setBusy(true);
    setError('');
    try {
      const task = await api.forkTask(boardId, {taskId: selectedTask.id, prompt, name, kind: 'side', model: selectedModel});
      await refreshTasks();
      setSelectedTask(task);
      setOptimisticPrompt({taskId: task.id, text: prompt, time: new Date().toISOString(), attachments: []});
      setShowSideTask(false);
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  async function refreshWorkspace() {
    await refreshTasks();
    setWatchRevision((revision) => revision + 1);
  }

  async function confirmDelete() {
    if (!deleteTarget || !boardId) return;
    setBusy(true);
    setError('');
    try {
      await api.deleteTasks(boardId, deleteTarget.taskIds);
      setDeleteTarget(null);
      setOpenMenu('');
      await refreshTasks();
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  function toggleProject(path: string) {
    setCollapsedProjects((current) => {
      const next = new Set(current);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }

  function selectTask(task: Task) {
    setRenamingTask(false);
    if (selectedTask?.id === task.id) setWatchRevision((revision) => revision + 1);
    else setSelectedTask(task);
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
    <div className={`studio-shell ${showInspector ? '' : 'inspector-hidden'} ${isMacOS ? 'platform-macos' : ''}`}>
      <header className="titlebar">
        <div className="brand-lockup"><div className="brand-mark" aria-label="Hobot Code">H</div><span>Hobot Code</span></div>
        <button className="board-switcher" onClick={() => setShowBoard(true)} disabled={busy}>
          <span className={`connection-dot ${connection ? 'online' : ''}`} />
          <span className="board-name">{connection?.board.name ?? 'Connect board'}</span>
          <ChevronDown size={14} />
        </button>
        <div className="titlebar-spacer" />
        {isMock() && <span className="preview-label">Preview</span>}
        <button className="icon-button" title="Refresh tasks" onClick={() => void refreshWorkspace()}><RefreshCw size={16} /></button>
        <button className={`icon-button ${showInspector ? 'active' : ''}`} title="Task details" onClick={() => setShowInspector((value) => !value)}><PanelRight size={17} /></button>
      </header>

      <aside className="task-sidebar">
        <div className="sidebar-heading">
          <div><span className="section-label">Projects</span><span className="task-count">{projects.length}</span></div>
          <button className="icon-button compact" title="New conversation" onClick={() => openNewTask()} disabled={!connection}><Plus size={17} /></button>
        </div>
        <label className="search-field"><Search size={15} /><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search projects and tasks" /></label>
        <div className="task-list">
          {projects.map((project) => <section className={`project-group ${collapsedProjects.has(project.path) ? 'collapsed' : ''}`} key={project.path}>
            <div className="project-heading">
              <button className="project-toggle" onClick={() => toggleProject(project.path)} title={collapsedProjects.has(project.path) ? 'Expand project' : 'Collapse project'}><ChevronRight className="project-chevron" size={13} /><Folder size={14} /><span>{project.name}</span><small>{project.tasks.length}</small></button>
              <button className="row-more project-add" title={`New conversation in ${project.name}`} onClick={() => openNewTask(project.path)}><Plus size={14} /></button>
              <button className="row-more" title="Project actions" onClick={() => setOpenMenu((current) => current === `project:${project.path}` ? '' : `project:${project.path}`)}><MoreHorizontal size={15} /></button>
              {openMenu === `project:${project.path}` && <div className="row-menu"><button onClick={() => setDeleteTarget({kind: 'project', label: project.name, taskIds: project.tasks.map((task) => task.id)})}><Trash2 size={14} />Remove project</button></div>}
            </div>
            {!collapsedProjects.has(project.path) && <div className="project-conversations">{arrangeTasks(project.tasks).map(({task, depth}) => <div key={task.id} className={`task-row-shell ${depth ? 'branch-task' : ''}`} style={{'--task-depth': depth} as any}>
              <button className={`task-row ${selectedTask?.id === task.id ? 'selected' : ''}`} onClick={() => {selectTask(task); setOpenMenu('');}}>
                {depth ? <CornerDownRight className="branch-mark" size={13} /> : <span className={`task-state-dot dot-${task.status}`} />}
                <span className="task-row-main"><span className="task-row-name">{task.name}</span>{depth > 0 && <span className="task-row-path">{task.branchKind === 'edit' ? 'Edited branch' : 'Side Agent'}</span>}</span>
                <span className="task-row-time">{relativeTime(task.updatedAt)}</span>
              </button>
              <button className="row-more task-more" title="Conversation actions" onClick={() => setOpenMenu((current) => current === `task:${task.id}` ? '' : `task:${task.id}`)}><MoreHorizontal size={15} /></button>
              {openMenu === `task:${task.id}` && <div className="row-menu task-menu"><button onClick={() => setDeleteTarget({kind: 'conversation', label: task.name, taskIds: [task.id]})}><Trash2 size={14} />Delete conversation</button></div>}
            </div>)}</div>}
          </section>)}
          {visibleTasks.length === 0 && <div className="empty-state"><ListTodo size={21} /><span>No tasks</span></div>}
        </div>
        {connection && <button className="board-summary" onClick={() => setShowBoard(true)}>
          <Server size={16} /><span><strong>{connection.board.name}</strong><small>{activeTaskCount}/{connection.daemon?.maximumTasks ?? 0} active</small></span><ChevronRight size={15} />
        </button>}
      </aside>

      <main className="task-main">
        {selectedTask ? <>
          <div className="task-header">
            <div className="task-title-block"><div className="task-title-line">{renamingTask ? <form className="title-editor" onSubmit={(event) => {event.preventDefault(); void renameSelectedTask();}}><input value={renameValue} maxLength={64} autoFocus onChange={(event) => setRenameValue(event.target.value)} onBlur={() => void renameSelectedTask()} onKeyDown={(event) => {if (event.key === 'Escape') {setRenameValue(selectedTask.name); setRenamingTask(false);}}} /></form> : <><h1>{selectedTask.name}</h1><button className="title-edit" title="Rename conversation" onClick={() => {setRenameValue(selectedTask.name); setRenamingTask(true);}}><FilePenLine size={13} /></button></>}<span className={`status status-${selectedTask.status}`}>{statusLabel[selectedTask.status] ?? selectedTask.status}</span></div><span className="workspace-path">{selectedTask.cwd}</span></div>
            <div className="task-actions">
              <button className="secondary-button side-task-button" title={selectedTask.sessionFile ? 'Create an independent agent from this conversation' : 'Side Agent is available after the first response'} onClick={() => setShowSideTask(true)} disabled={busy || !selectedTask.sessionFile}><GitBranch size={15} />Side Agent</button>
              {terminalStatuses.has(selectedTask.status) && <button className="secondary-button" onClick={() => composerRef.current?.focus()}><RefreshCw size={14} />{selectedComposerMode === 'resume' ? 'Resume' : 'New session'}</button>}
            </div>
          </div>

          <div className="conversation" ref={timelineRef} onScroll={onTimelineScroll} aria-label="Conversation">
            <div className="conversation-inner">
              {eventsLoading && events.length === 0 && <div className="loading-conversation"><LoaderCircle size={18} className="spin" /><span>Loading conversation</span></div>}
              {!eventsLoading && conversation.length === 0 && <div className="empty-conversation"><div className="empty-symbol"><MessageSquare size={22} /></div><strong>Start a conversation</strong><span>Ask Hobot Code to inspect, build, debug, or deploy on this board.</span></div>}
              {conversation.map((item) => item.kind === 'user'
                ? <UserMessage key={item.key} item={item} onEdit={editMessage} />
                : <AssistantTurn key={item.key} item={item} running={selectedTask.status === 'running' && !optimisticPrompt && item === conversation[conversation.length - 1]} />)}
              {optimisticPrompt?.taskId === selectedTask.id && !events.some((entry) => entry.normalized?.type === 'user.message' && String(entry.normalized.data?.text ?? '') === optimisticPrompt.text) && <UserMessage item={{kind: 'user', key: 'optimistic', sequence: Number.MAX_SAFE_INTEGER, time: optimisticPrompt.time, text: optimisticPrompt.text, attachments: optimisticPrompt.attachments.map((image) => ({name: image.name, mimeType: image.mimeType, preview: imageDataURL(image)}))}} />}
              {['starting', 'running'].includes(selectedTask.status) && <AgentProgress startedAt={activityStart} now={activityClock} hasOutput={!optimisticPrompt && latestConversationItem?.kind === 'assistant' && Boolean(latestConversationItem.text || latestConversationItem.thinking || latestConversationItem.tools.length)} />}
            </div>
          </div>

          {hasNewOutput && <button className="jump-latest" onClick={scrollToLatest}><ArrowDown size={15} />New output</button>}
          <div className="composer-dock">
            {activeApproval && <ApprovalBar approval={activeApproval} busy={busy} respond={(response) => respond(activeApproval, response)} />}
            <form className="composer" onSubmit={submitPrompt}>
              {editingMessage !== null && <div className="editing-banner"><GitBranch size={14} /><span>Continue from this message in a new branch</span><button type="button" title="Cancel edit" onClick={() => {setEditingMessage(null); setComposer('');}}><X size={14} /></button></div>}
              {attachments.length > 0 && <div className="attachment-tray">{attachments.map((image, index) => <div className="attachment-chip" key={`${image.name}-${index}`}><img src={imageDataURL(image)} alt="" /><span>{image.name || `Image ${index + 1}`}</span><button type="button" title="Remove image" onClick={() => setAttachments((current) => current.filter((_, itemIndex) => itemIndex !== index))}><X size={13} /></button></div>)}</div>}
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
                rows={1}
              />
              <div className="composer-footer">
                <ImagePickerButton disabled={composerBlocked || attachments.length >= 4} onPick={(images) => {try {setAttachments(appendImages(attachments, images));} catch (reason) {setError(friendlyError(String(reason)));}}} onError={setError} />
                <label className="model-picker" title={canChangeModel ? 'Choose model' : 'Stop the current turn before changing models'}><select aria-label="Model" value={modelPickerValue} disabled={!canChangeModel} onChange={(event) => void changeModel(event.target.value)}><option value="" disabled>Board default</option>{selectedModel.startsWith('drobotics/') && !models.some((model) => `${model.provider}/${model.id}` === selectedModel) && <option value={selectedModel}>{selectedModel.split('/').at(-1)}</option>}{models.map((model) => <option key={`${model.provider}/${model.id}`} value={`${model.provider}/${model.id}`}>{model.name || model.id}</option>)}</select><ChevronDown size={12} /></label>
                <label className="permission-picker" title={canChangePermissions ? 'Choose approval mode' : 'Stop the current turn before changing permissions'}><ShieldCheck size={13} /><select aria-label="Approval mode" value={selectedPermissionMode} disabled={!canChangePermissions} onChange={(event) => void changePermissionMode(event.target.value)}><option value="review">Review only</option><option value="ask">Ask for changes</option><option value="developer">Developer</option></select><ChevronDown size={12} /></label>
                <span className="composer-state">{editingMessage !== null ? 'Creates a branch' : selectedComposerMode === 'resume' ? 'Resume session' : selectedComposerMode === 'restart' ? 'New session' : statusLabel[selectedTask.status] ?? selectedTask.status}</span>
                {canStopTask ? <button className="send-button stop-mode" type="button" title="Stop" onClick={stopTask} disabled={busy || selectedTask.status === 'stopping'}><Square size={14} fill="currentColor" /></button> : <button className="send-button" type="submit" title="Send" disabled={!composer.trim() || composerBlocked}><ArrowUp size={17} /></button>}
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
      {showNewTask && connection && <NewTaskDialog boardId={connection.board.id} initialCwd={newTaskProject} busy={busy} onClose={() => setShowNewTask(false)} onCreate={async (request) => {setBusy(true); setError(''); try {const task = await api.startTask(connection.board.id, request); await refreshTasks(); setSelectedTask(task); setShowNewTask(false);} catch (reason) {setError(String(reason));} finally {setBusy(false);}}} />}
      {showSideTask && selectedTask && <SideTaskDialog parent={selectedTask} busy={busy} onClose={() => setShowSideTask(false)} onCreate={createSideTask} />}
      {deleteTarget && <DeleteDialog target={deleteTarget} busy={busy} onClose={() => setDeleteTarget(null)} onDelete={confirmDelete} />}
    </div>
  );
}

function UserMessage({item, onEdit}: {item: UserConversationItem; onEdit?: (item: UserConversationItem) => void}) {
  return <article className="user-message">{item.attachments.length > 0 && <div className="message-attachments">{item.attachments.map((attachment, index) => attachment.preview ? <img key={`${attachment.name}-${index}`} src={attachment.preview} alt={attachment.name || `Attached image ${index + 1}`} /> : <span key={`${attachment.name}-${index}`}><Paperclip size={12} />{attachment.name || attachment.mimeType}</span>)}</div>}<div className="user-message-content">{item.text}</div><div className="message-actions"><time>{formatTime(item.time)}</time><CopyButton value={item.text} />{onEdit && <button className="copy-button" title="Edit from this point" onClick={() => onEdit(item)}><FilePenLine size={14} /></button>}</div></article>;
}

function AssistantTurn({item, running}: {item: AssistantConversationItem; running: boolean}) {
  return <article className="assistant-turn">
    {item.thinking && <ThinkingBlock item={item} running={running} />}
    {item.tools.length > 0 && <ToolGroup tools={item.tools} />}
    {item.notices.map((notice, index) => <div key={`${notice.time}-${index}`} className={`turn-notice notice-${notice.type}`}><Activity size={13} /><span>{notice.label}</span></div>)}
    {item.text && <div className="assistant-content"><MarkdownContent value={item.text} /><div className="assistant-actions"><CopyButton value={item.text} /></div></div>}
    {running && <div className="agent-progress"><LoaderCircle size={14} className="spin" /><span>Working</span></div>}
  </article>;
}

function AgentProgress({startedAt, now, hasOutput}: {startedAt?: string; now: number; hasOutput: boolean}) {
  if (hasOutput) return null;
  const seconds = startedAt ? Math.max(0, Math.floor((now - new Date(startedAt).getTime()) / 1000)) : 0;
  const label = seconds < 2 ? 'Sending' : seconds < 8 ? 'Starting' : seconds < 30 ? 'Thinking' : 'Still working';
  return <div className="agent-progress immediate"><LoaderCircle size={15} className="spin" /><span>{label}</span>{seconds >= 2 && <small>{seconds}s</small>}</div>;
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
    a: ({node: _node, href, ...props}: any) => <a {...props} href={href} onClick={(event) => {
      event.preventDefault();
      if (href) void api.openExternalURL(href);
    }} />,
  }}>{value}</ReactMarkdown></div>;
}

function ImagePickerButton({disabled, onPick, onError}: {disabled: boolean; onPick: (images: ImageContent[]) => void; onError: (message: string) => void}) {
  const input = useRef<HTMLInputElement>(null);
  return <><button className="icon-button compact attach-button" type="button" title="Attach images" disabled={disabled} onClick={() => input.current?.click()}><Paperclip size={15} /></button><input ref={input} className="file-input" type="file" accept="image/jpeg,image/png,image/webp,image/gif" multiple onChange={(event) => {const files = [...(event.target.files ?? [])]; event.target.value = ''; void Promise.all(files.map(prepareImage)).then(onPick).catch((reason) => onError(friendlyError(String(reason))));}} /></>;
}

function appendImages(current: ImageContent[], next: ImageContent[]): ImageContent[] {
  const combined = [...current, ...next];
  if (combined.length > 4) throw new Error('A message can contain at most 4 images.');
  const bytes = combined.reduce((total, image) => total + Math.floor(image.data.length * 3 / 4), 0);
  if (bytes > 900 * 1024) throw new Error('Attached images are too large. Remove an image and try again.');
  return combined;
}

async function prepareImage(file: File): Promise<ImageContent> {
  if (!['image/jpeg', 'image/png', 'image/webp', 'image/gif'].includes(file.type)) throw new Error(`${file.name} is not a supported image.`);
  if (file.size > 20 * 1024 * 1024) throw new Error(`${file.name} exceeds the 20 MB source limit.`);
  if (file.type === 'image/gif' && file.size <= 800 * 1024) return imageFromDataURL(file.name, file.type, await readFileAsDataURL(file));
  if (file.size <= 700 * 1024) return imageFromDataURL(file.name, file.type, await readFileAsDataURL(file));

  const source = await loadBrowserImage(file);
  const scale = Math.min(1, 1600 / Math.max(source.naturalWidth, source.naturalHeight));
  const canvas = document.createElement('canvas');
  canvas.width = Math.max(1, Math.round(source.naturalWidth * scale));
  canvas.height = Math.max(1, Math.round(source.naturalHeight * scale));
  const context = canvas.getContext('2d');
  if (!context) throw new Error(`Could not prepare ${file.name}.`);
  context.drawImage(source, 0, 0, canvas.width, canvas.height);
  for (const quality of [0.86, 0.72, 0.58, 0.44]) {
    const dataURL = canvas.toDataURL('image/jpeg', quality);
    const image = imageFromDataURL(file.name.replace(/\.[^.]+$/, '') + '.jpg', 'image/jpeg', dataURL);
    if (Math.floor(image.data.length * 3 / 4) <= 800 * 1024) return image;
  }
  throw new Error(`${file.name} could not be reduced below the upload limit.`);
}

function readFileAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error ?? new Error(`Could not read ${file.name}.`));
    reader.readAsDataURL(file);
  });
}

function loadBrowserImage(file: File): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file);
    const image = new Image();
    image.onload = () => { URL.revokeObjectURL(url); resolve(image); };
    image.onerror = () => { URL.revokeObjectURL(url); reject(new Error(`Could not decode ${file.name}.`)); };
    image.src = url;
  });
}

function imageFromDataURL(name: string, mimeType: string, value: string): ImageContent {
  const marker = value.indexOf(',');
  if (marker < 0) throw new Error(`Could not encode ${name}.`);
  return {type: 'image', data: value.slice(marker + 1), mimeType, name};
}

function imageDataURL(image: ImageContent): string { return `data:${image.mimeType};base64,${image.data}`; }

function ApprovalBar({approval, busy, respond}: {approval: Approval; busy: boolean; respond: (response: Record<string, unknown>) => void}) {
  const view = approvalPresentation(approval);
  return <section className="approval-bar" role="alert" aria-label={view.title}><div className="approval-heading"><div className="approval-icon"><ShieldCheck size={17} /></div><strong>{view.title}</strong></div><pre className="approval-detail">{view.detail}</pre>{view.remembersExactCall && <div className="approval-scope"><ShieldCheck size={13} />Remembering applies only to this exact tool call in this task.</div>}<div className="approval-actions">{approval.method === 'select' ? approval.options?.map((option) => <button key={option} className={option === 'Allow once' ? 'primary-button' : 'secondary-button'} disabled={busy} onClick={() => respond({value: option})}>{option}</button>) : <><button className="secondary-button" disabled={busy} onClick={() => respond({confirmed: false})}>Deny</button><button className="primary-button" disabled={busy} onClick={() => respond({confirmed: true})}>Allow once</button></>}</div></section>;
}

function BoardDialog({boards, busy, onClose, onConnect, onSave}: {boards: Board[]; busy: boolean; onClose: () => void; onConnect: (board: Board) => void; onSave: (board: Board) => Promise<void>}) {
  const [editing, setEditing] = useState(boards.length === 0);
  const [form, setForm] = useState<Board>({id: '', name: 'RDK S100', host: '10.112.10.98', user: 'root', port: 22, identityFile: ''});
  const availablePresets = boardPresets.filter((preset) => !boards.some((board) => board.host === preset.host));
  return <div className="modal-backdrop"><div className="modal board-modal"><div className="modal-header"><div><span className="modal-eyebrow">Boards</span><h2>{editing ? 'Add board' : 'Connect'}</h2></div>{boards.length > 0 && <button className="icon-button" title="Close" onClick={onClose}><X size={18} /></button>}</div>{!editing ? <><div className="saved-boards">{boards.map((board) => <button key={board.id} className="saved-board" onClick={() => onConnect(board)} disabled={busy}><Server size={19} /><span><strong>{board.name}</strong><small>{board.user}@{board.host}:{board.port}</small></span><ChevronRight size={15} /></button>)}</div><button className="add-board-row" onClick={() => setEditing(true)}><Plus size={16} />Add board</button></> : <form onSubmit={(event) => {event.preventDefault(); void onSave(form);}} className="form-grid">{availablePresets.length > 0 && <div className="board-presets">{availablePresets.map((preset) => <button type="button" key={preset.host} className={form.host === preset.host ? 'selected' : ''} onClick={() => setForm({id: '', ...preset, identityFile: form.identityFile})}><Server size={14} /><span>{preset.name}</span></button>)}</div>}<label><span>Name</span><input value={form.name} onChange={(event) => setForm({...form, name: event.target.value})} required /></label><label><span>Host</span><input value={form.host} onChange={(event) => setForm({...form, host: event.target.value})} required /></label><div className="form-row"><label><span>User</span><input value={form.user} onChange={(event) => setForm({...form, user: event.target.value})} required /></label><label><span>Port</span><input type="number" min="1" max="65535" value={form.port} onChange={(event) => setForm({...form, port: Number(event.target.value)})} required /></label></div><label><span>Identity file</span><input value={form.identityFile} onChange={(event) => setForm({...form, identityFile: event.target.value})} placeholder="Use SSH agent or config" /></label><div className="modal-actions">{boards.length > 0 && <button type="button" className="secondary-button" onClick={() => setEditing(false)}>Back</button>}<button className="primary-button" type="submit" disabled={busy}>{busy ? <LoaderCircle size={15} className="spin" /> : <Server size={15} />}Save & connect</button></div></form>}</div></div>;
}

function NewTaskDialog({boardId, initialCwd, busy, onClose, onCreate}: {boardId: string; initialCwd: string; busy: boolean; onClose: () => void; onCreate: (request: {name: string; cwd: string; prompt: string; images?: ImageContent[]; approve: boolean; model?: string; permissionMode?: string}) => void}) {
  const [request, setRequest] = useState<{name: string; cwd: string; prompt: string; images: ImageContent[]; approve: boolean; permissionMode: string}>({name: '', cwd: initialCwd, prompt: '', images: [], approve: false, permissionMode: 'ask'});
  const [listing, setListing] = useState<WorkspaceListing | null>(null);
  const [folderMode, setFolderMode] = useState(false);
  const [newFolder, setNewFolder] = useState('');
  const [dialogError, setDialogError] = useState('');
  const activeBoard = useRef<string>(boardId);
  const browse = useCallback(async (path = '') => {
    setDialogError('');
    try {
      const next = await api.browseWorkspace(activeBoard.current || boardId, path);
      setListing(next);
      if (!request.cwd) setRequest((current) => ({...current, cwd: next.home}));
    } catch (reason) {
      setDialogError(friendlyError(String(reason)));
    }
  }, [request.cwd]);
  useEffect(() => { void browse(initialCwd); }, []);
  return <div className="modal-backdrop"><form className="modal task-modal" onSubmit={(event) => {event.preventDefault(); if (request.cwd) onCreate(request);}}><div className="modal-header"><div><span className="modal-eyebrow">New conversation</span><h2>{initialCwd ? `New task in ${basename(initialCwd)}` : 'Start a task'}</h2></div><button type="button" className="icon-button" title="Close" onClick={onClose}><X size={18} /></button></div><div className="form-grid"><label><span>Title</span><input value={request.name} maxLength={64} onChange={(event) => setRequest({...request, name: event.target.value})} placeholder="Automatically generated from your instruction" /></label><div className="workspace-chooser"><span>Project</span><button type="button" className="workspace-selection" onClick={() => setFolderMode(true)}><FolderOpen size={16} /><span><strong>{request.cwd === listing?.home ? 'No project folder' : basename(request.cwd) || 'Choose a folder'}</strong><small>{request.cwd || 'Select an existing or new folder'}</small></span><ChevronRight size={15} /></button></div><label><span>Instruction</span><textarea rows={5} value={request.prompt} onChange={(event) => setRequest({...request, prompt: event.target.value})} required autoFocus /></label>{request.images.length > 0 && <div className="attachment-tray modal-attachments">{request.images.map((image, index) => <div className="attachment-chip" key={`${image.name}-${index}`}><img src={imageDataURL(image)} alt="" /><span>{image.name}</span><button type="button" title="Remove image" onClick={() => setRequest({...request, images: request.images.filter((_, itemIndex) => itemIndex !== index)})}><X size={13} /></button></div>)}</div>}<div className="new-task-tools"><ImagePickerButton disabled={busy || request.images.length >= 4} onPick={(images) => {try {setRequest({...request, images: appendImages(request.images, images)});} catch (reason) {setDialogError(friendlyError(String(reason)));}}} onError={(message) => setDialogError(friendlyError(message))} /><span>Images</span></div><label><span>Approval mode</span><select value={request.permissionMode} onChange={(event) => setRequest({...request, permissionMode: event.target.value})}><option value="review">Review only</option><option value="ask">Ask for changes</option><option value="developer">Developer</option></select></label><label className="checkbox-row"><input type="checkbox" checked={request.approve} onChange={(event) => setRequest({...request, approve: event.target.checked})} /><span>Trust project resources</span></label>{dialogError && <div className="inline-error">{dialogError}</div>}<div className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>Cancel</button><button className="primary-button" disabled={busy || !request.prompt.trim() || !request.cwd}><Plus size={15} />Create task</button></div></div>{folderMode && listing && <div className="folder-panel"><div className="folder-panel-header"><button type="button" className="icon-button compact" title="Back" onClick={() => setFolderMode(false)}><ArrowLeft size={16} /></button><div><strong>Choose project folder</strong><small>{listing.path}</small></div></div><button type="button" className="folder-choice special" onClick={() => {setRequest({...request, cwd: listing.home}); setFolderMode(false);}}><MessageSquare size={16} /><span><strong>No project folder</strong><small>Use {listing.home} as a neutral workspace</small></span></button>{listing.parent && <button type="button" className="folder-choice" onClick={() => void browse(listing.parent)}><ArrowLeft size={15} /><span><strong>Parent folder</strong><small>{listing.parent}</small></span></button>}<div className="folder-list">{listing.directories.map((entry) => <button type="button" className="folder-choice" key={entry.path} onDoubleClick={() => {setRequest({...request, cwd: entry.path}); setFolderMode(false);}} onClick={() => void browse(entry.path)}><Folder size={16} /><span><strong>{entry.name}</strong><small>{entry.path}</small></span><ChevronRight size={14} /></button>)}</div><div className="new-folder-row"><input value={newFolder} onChange={(event) => setNewFolder(event.target.value)} placeholder="New folder name" /><button type="button" className="secondary-button" disabled={!newFolder.trim()} onClick={() => void api.createWorkspace(activeBoard.current || boardId, listing.path, newFolder.trim()).then((next) => {setListing(next); setRequest({...request, cwd: next.path}); setNewFolder(''); setFolderMode(false);}).catch((reason) => setDialogError(friendlyError(String(reason))))}>Create</button></div><div className="folder-panel-actions"><button type="button" className="primary-button" onClick={() => {setRequest({...request, cwd: listing.path}); setFolderMode(false);}}>Choose this folder</button></div></div>}</form></div>;
}

function SideTaskDialog({parent, busy, onClose, onCreate}: {parent: Task; busy: boolean; onClose: () => void; onCreate: (prompt: string, name: string) => void}) {
  const [prompt, setPrompt] = useState('');
  const [name, setName] = useState('');
  return <div className="modal-backdrop"><form className="modal side-task-modal" onSubmit={(event) => {event.preventDefault(); onCreate(prompt.trim(), name.trim());}}><div className="modal-header"><div><span className="modal-eyebrow">Parallel branch</span><h2>New side task</h2></div><button type="button" className="icon-button" title="Close" onClick={onClose}><X size={18} /></button></div><div className="fork-source"><GitBranch size={16} /><span><strong>Shares the settled context from {parent.name}</strong><small>The main task stays unchanged and both can continue independently.</small></span></div><div className="form-grid"><label><span>Name</span><input value={name} onChange={(event) => setName(event.target.value)} placeholder={`${parent.name}-side`} /></label><label><span>Instruction</span><textarea rows={6} value={prompt} onChange={(event) => setPrompt(event.target.value)} autoFocus required /></label><div className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>Cancel</button><button className="primary-button" disabled={busy || !prompt.trim()}><GitBranch size={15} />Start side task</button></div></div></form></div>;
}

function DeleteDialog({target, busy, onClose, onDelete}: {target: {kind: 'conversation' | 'project'; label: string; taskIds: string[]}; busy: boolean; onClose: () => void; onDelete: () => void}) {
  const project = target.kind === 'project';
  return <div className="modal-backdrop"><div className="modal confirm-modal"><div className="confirm-icon"><Trash2 size={18} /></div><h2>{project ? 'Remove project?' : 'Delete conversation?'}</h2><p>{project ? `This removes ${target.taskIds.length} conversation${target.taskIds.length === 1 ? '' : 's'} from ${target.label}.` : `This permanently removes ${target.label} from Hobot Code.`}</p><small>Running agents will stop. Files in the board workspace will not be deleted.</small><div className="modal-actions"><button className="secondary-button" onClick={onClose} disabled={busy}>Cancel</button><button className="danger-button" onClick={onDelete} disabled={busy}>{busy ? <LoaderCircle size={15} className="spin" /> : <Trash2 size={15} />}Delete</button></div></div></div>;
}

function InspectorSection({title, children}: {title: string; children: ReactNode}) { return <section className="inspector-section"><h3>{title}</h3>{children}</section>; }
function InfoRow({label, value, mono, copy}: {label: string; value: string; mono?: boolean; copy?: string}) { return <div className="info-row"><span>{label}</span><div><strong className={mono ? 'mono' : ''}>{value}</strong>{copy && <CopyButton value={copy} />}</div></div>; }
function CopyButton({value}: {value: string}) { const [copied, setCopied] = useState(false); return <button type="button" className="copy-button" title={copied ? 'Copied' : 'Copy'} onClick={() => void navigator.clipboard.writeText(value).then(() => {setCopied(true); window.setTimeout(() => setCopied(false), 1200);})}>{copied ? <Check size={13} /> : <Clipboard size={13} />}</button>; }
function Metric({icon, label, value}: {icon: ReactNode; label: string; value: string}) { return <div className="metric"><span>{icon}{label}</span><strong>{value}</strong></div>; }
function formatTime(value: string) { return new Date(value).toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'}); }
function relativeTime(value: string) { const seconds = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 1000)); if (seconds < 60) return 'now'; if (seconds < 3600) return `${Math.floor(seconds / 60)}m`; if (seconds < 86_400) return `${Math.floor(seconds / 3600)}h`; return `${Math.floor(seconds / 86_400)}d`; }
function basename(path: string) { return path.split('/').filter(Boolean).at(-1) ?? path; }
function friendlyError(value: string) {
  const message = value.replace(/^Error:\s*/i, '').replace(/^task_[a-z_]+:\s*/i, '');
  if (/context deadline exceeded|operation timed out|connect to host .* timed out/i.test(message)) return 'Could not reach the board. Check the network or VPN and try again.';
  if (/requires a newer Hobot Code event schema/i.test(message)) return 'Update the board-side Hobot Code and reconnect.';
  if (/has no resumable Hobot Code session/i.test(message)) return 'This task has no saved session. Start a new session instead.';
  if (/background task limit reached.*all agents are currently working/i.test(message)) return 'Both background slots are busy. Stop a working agent, or wait for one to finish.';
  return message;
}

export default App;
