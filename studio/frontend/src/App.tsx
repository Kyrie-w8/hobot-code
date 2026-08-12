import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import type {FormEvent, ReactNode, UIEvent} from 'react';
import ReactMarkdown from 'react-markdown';
import {
  Activity, AlertTriangle, ArrowDown, ArrowUp, Bot, Box, Brain, Check, ChevronDown,
  ChevronRight, Clipboard, CornerDownRight, Cpu, FilePenLine, Folder,
  GitBranch, ListTodo, LoaderCircle, MessageSquare,
  Download, Gauge, MoreHorizontal, PanelRight, Paperclip, Plus, RefreshCw, Search, Server, ShieldCheck,
  Square, SquareTerminal, Trash2, Wrench, X, XCircle,
} from 'lucide-react';
import {api, isMock} from './api';
import type {TaskWatchStatus} from './api';
import {composerIsBlocked, composerMode, shouldSubmitComposer, terminalStatuses} from './composer-policy.js';
import {buildConversation, elapsedLabel, recentEventsAfter} from './conversation-model.js';
import {approvalPresentation, approvalResponse} from './approval-model.js';
import {acceleratorMemoryMetrics, activeDDRBandwidth, boardHealth, bpuCoreLabel, bpuFrequency, bpuTemperature, bpuUnavailableReason, bpuUtilization, durationLabel, formatBytes, orphanedIONNotice, percentLabel, systemResourceMetrics} from './board-health.js';
import {arrangeTasks, groupTasksByProject} from './project-model.js';
import {markdownRemarkPlugins} from './markdown-config.js';
import {effectiveModel as resolveEffectiveModel, modelAcceptsImages} from './model-capabilities.js';
import {currentModelHealth as resolveCurrentModelHealth, modelHealthLabel} from './model-health.js';
import {rdkWorkflows} from './rdk-workflows.js';
import {deploymentCanStart, deploymentCompatibilityLabel, deploymentPhaseLabel, deploymentProfileFor, preferredDeploymentArtifact} from './deployment-model.js';
import {shouldToggleMaximise} from './titlebar-policy.js';
import {isCurrentRequest, isCurrentTarget, watchRetryDelay, watchStatusLabel} from './async-policy.js';
import type {AssistantConversationItem, ToolActivity, UserConversationItem} from './conversation-model.js';
import type {Approval, Board, Connection, DeploymentInspection, DeploymentStatus, ImageContent, ModelHealth, ModelOption, StartDeploymentRequest, SystemSnapshot, Task, TaskEvent} from './types';
import './App.css';

const isMacOS = typeof navigator !== 'undefined' && /Mac/.test(navigator.platform);

const statusLabel: Record<string, string> = {
  draft: 'Draft',
  starting: 'Starting', idle: 'Ready', running: 'Working', waiting: 'Approval needed',
  stopping: 'Stopping', stopped: 'Stopped', failed: 'Failed', interrupted: 'Interrupted',
};

const boardPresets: Array<Pick<Board, 'name' | 'user' | 'port'>> = [
  {name: 'RDK S100', user: 'root', port: 22},
  {name: 'RDK S600', user: 'root', port: 22},
  {name: 'RDK X5', user: 'root', port: 22},
];

function App() {
  const [boards, setBoards] = useState<Board[]>([]);
  const [connection, setConnection] = useState<Connection | null>(null);
  const [connectionState, setConnectionState] = useState<'connecting' | 'online' | 'offline'>('offline');
  const [snapshot, setSnapshot] = useState<SystemSnapshot | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  const [events, setEvents] = useState<TaskEvent[]>([]);
  const [search, setSearch] = useState('');
  const [composer, setComposer] = useState('');
  const [editingMessage, setEditingMessage] = useState<number | null>(null);
  const [models, setModels] = useState<ModelOption[]>([]);
  const [modelHealth, setModelHealth] = useState<ModelHealth | null>(null);
  const [checkingModel, setCheckingModel] = useState(false);
  const [attachments, setAttachments] = useState<ImageContent[]>([]);
  const [optimisticPrompt, setOptimisticPrompt] = useState<{taskId: string; text: string; time: string; attachments: ImageContent[]} | null>(null);
  const [showSideTask, setShowSideTask] = useState(false);
  const [showDeployment, setShowDeployment] = useState(false);
  const [deploymentStatus, setDeploymentStatus] = useState<DeploymentStatus | null>(null);
  const [activityClock, setActivityClock] = useState(Date.now());
  const [busy, setBusy] = useState(false);
  const [eventsLoading, setEventsLoading] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [showBoard, setShowBoard] = useState(false);
  const [showInspector, setShowInspector] = useState(false);
  const [collapsedProjects, setCollapsedProjects] = useState<Set<string>>(new Set());
  const [openMenu, setOpenMenu] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<{kind: 'conversation' | 'project'; label: string; taskIds: string[]} | null>(null);
  const [renamingTask, setRenamingTask] = useState(false);
  const [renameValue, setRenameValue] = useState('');
  const [watchRevision, setWatchRevision] = useState(0);
  const [watchStatus, setWatchStatus] = useState<TaskWatchStatus | null>(null);
  const [hasNewOutput, setHasNewOutput] = useState(false);
  const startupStarted = useRef(false);
  const timelineRef = useRef<HTMLDivElement>(null);
  const composerRef = useRef<HTMLTextAreaElement>(null);
  const followsOutput = useRef(true);
  const taskDrafts = useRef(new Map<string, {text: string; editingMessage: number | null; attachments: ImageContent[]}>());
  const previousTaskId = useRef('');
  const activeBoardId = useRef('');
  const selectedTaskId = useRef('');
  const snapshotSampling = useRef(false);
  const modelHealthRequest = useRef(0);
  const connectionRequest = useRef(0);
  const connectionTarget = useRef('');
  const watchRetryAttempt = useRef(0);
  const watchRetryTimer = useRef<number | null>(null);

  const boardId = connection?.board.id ?? '';

  const refreshTasks = useCallback(async (targetBoard = boardId) => {
    if (!targetBoard) return;
    const expectedTask = selectedTaskId.current;
    try {
      const page = await api.tasks(targetBoard);
      if (targetBoard !== activeBoardId.current) return;
      setTasks(page.tasks ?? []);
      if (expectedTask.startsWith('draft:') || selectedTaskId.current !== expectedTask) return;
      const summary = page.tasks?.find((task) => task.id === expectedTask) ?? page.tasks?.[0] ?? null;
      selectedTaskId.current = summary?.id ?? '';
      setSelectedTask(summary);
      if (summary) {
        const detail = await api.task(targetBoard, summary.id);
        if (targetBoard === activeBoardId.current && selectedTaskId.current === summary.id) setSelectedTask(detail);
      }
    } catch (reason) {
      if (targetBoard === activeBoardId.current) setError(String(reason));
    }
  }, [boardId]);

  const connect = useCallback(async (board: Board) => {
    const request = ++connectionRequest.current;
    connectionTarget.current = board.id;
    modelHealthRequest.current += 1;
    setCheckingModel(false);
    setModelHealth(null);
    setBusy(true);
    setConnectionState('connecting');
    setError('');
    try {
      const previousBoard = activeBoardId.current;
      const next = await api.connectBoard(board.id);
      const [pageModels, page] = await Promise.all([
        api.models(board.id).catch(() => []),
        api.tasks(board.id),
      ]);
      const initialTask = page.tasks?.[0] ?? null;
      const [initialDetail, nextSnapshot] = await Promise.all([
        initialTask ? api.task(board.id, initialTask.id) : Promise.resolve(null),
        next.snapshot
          ? Promise.resolve(next.snapshot)
          : next.capabilities?.capabilities.includes('system.snapshot')
            ? api.systemSnapshot(board.id).catch(() => null)
            : Promise.resolve(null),
      ]);
      if (!isCurrentRequest(request, connectionRequest.current)) {
        if (board.id !== connectionTarget.current && board.id !== activeBoardId.current) {
          await api.disconnectBoard(board.id).catch(() => undefined);
        }
        return;
      }
      activeBoardId.current = board.id;
      if (previousBoard && previousBoard !== board.id) await api.disconnectBoard(previousBoard).catch(() => undefined);
      setConnection(next);
      setConnectionState('online');
      setModels(pageModels ?? []);
      setTasks(page.tasks ?? []);
      selectedTaskId.current = initialTask?.id ?? '';
      setSelectedTask(initialDetail ?? initialTask);
      setSnapshot(nextSnapshot);
      setEvents([]);
      setOptimisticPrompt(null);
      setError('');
      setShowBoard(false);
    } catch (reason) {
      if (!isCurrentRequest(request, connectionRequest.current)) return;
      setConnectionState('offline');
      setError(String(reason));
    } finally {
      if (isCurrentRequest(request, connectionRequest.current)) setBusy(false);
    }
  }, []);

  const scheduleWatchRetry = useCallback((targetBoard: string, targetTask: string, message: string) => {
    if (targetBoard !== activeBoardId.current || targetTask !== selectedTaskId.current || watchRetryTimer.current !== null) return;
    const attempt = ++watchRetryAttempt.current;
    const delay = watchRetryDelay(attempt);
    setWatchStatus({boardId: targetBoard, taskId: targetTask, state: 'failed', attempt, message});
    watchRetryTimer.current = window.setTimeout(() => {
      watchRetryTimer.current = null;
      if (targetBoard === activeBoardId.current && targetTask === selectedTaskId.current) setWatchRevision((revision) => revision + 1);
    }, delay);
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
      if (watchError.boardId === activeBoardId.current && watchError.taskId === selectedTask?.id) {
        scheduleWatchRetry(watchError.boardId, watchError.taskId, watchError.error);
      }
    });
    const removeStatus = api.onWatchStatus((status) => {
      if (status.boardId !== activeBoardId.current || status.taskId !== selectedTask?.id) return;
      if (status.state === 'connected') {
        watchRetryAttempt.current = 0;
        if (watchRetryTimer.current !== null) window.clearTimeout(watchRetryTimer.current);
        watchRetryTimer.current = null;
        setWatchStatus(null);
      } else if (status.state === 'failed') {
        scheduleWatchRetry(status.boardId, status.taskId, status.message ?? 'The live event stream stopped.');
      } else {
        setWatchStatus(status);
      }
    });
    return () => { removeEvent(); removeError(); removeStatus(); };
  }, [boardId, refreshTasks, scheduleWatchRetry, selectedTask?.id]);

  useEffect(() => {
    watchRetryAttempt.current = 0;
    if (watchRetryTimer.current !== null) window.clearTimeout(watchRetryTimer.current);
    watchRetryTimer.current = null;
    return () => {
      if (watchRetryTimer.current !== null) window.clearTimeout(watchRetryTimer.current);
      watchRetryTimer.current = null;
    };
  }, [boardId, selectedTask?.id]);

  useEffect(() => {
    followsOutput.current = true;
    setHasNewOutput(false);
    setWatchStatus(watchRetryAttempt.current > 0 && boardId && selectedTask
      ? {boardId, taskId: selectedTask.id, state: 'reconnecting', attempt: watchRetryAttempt.current, message: 'Recovering live updates.'}
      : null);
    if (!boardId || !selectedTask || selectedTask.id.startsWith('draft:')) {
      setEvents([]);
      setEventsLoading(false);
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
    }).catch((reason) => {
      if (active) scheduleWatchRetry(boardId, selectedTask.id, String(reason));
    }).finally(() => {
      if (active) setEventsLoading(false);
    });
    return () => {
      active = false;
      void api.stopWatch(boardId, selectedTask.id);
    };
  }, [boardId, scheduleWatchRetry, selectedTask?.id, watchRevision]);

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
    if (!boardId) return;
    const timer = window.setInterval(() => {
      api.refreshBoard(boardId).then((nextConnection) => {
        if (activeBoardId.current !== boardId) return;
        setConnection(nextConnection);
        setConnectionState('online');
        if (nextConnection.reconnected) setWatchRevision((revision) => revision + 1);
      }).catch(() => {
        if (activeBoardId.current === boardId) setConnectionState('offline');
      });
    }, 15000);
    return () => window.clearInterval(timer);
  }, [boardId]);

  useEffect(() => {
    if (!boardId || !connection?.capabilities?.capabilities.includes('system.snapshot')) return;
    const sample = () => {
      if (snapshotSampling.current) return;
      snapshotSampling.current = true;
      void api.systemSnapshot(boardId).then((value) => {
        if (activeBoardId.current === boardId) setSnapshot(value);
      }).catch(() => undefined).finally(() => { snapshotSampling.current = false; });
    };
    const timer = window.setInterval(sample, showInspector ? 2000 : 30000);
    return () => window.clearInterval(timer);
  }, [boardId, connection?.capabilities, showInspector]);

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
    if (!modelHealth) return;
    const remaining = Date.parse(modelHealth.expiresAt) - Date.now();
    if (remaining <= 0) {
      setModelHealth(null);
      return;
    }
    const timer = window.setTimeout(() => setModelHealth((current) => current?.expiresAt === modelHealth.expiresAt ? null : current), remaining);
    return () => window.clearTimeout(timer);
  }, [modelHealth]);

  useEffect(() => {
    const textarea = composerRef.current;
    if (!textarea) return;
    textarea.style.height = 'auto';
    textarea.style.height = `${Math.min(180, Math.max(44, textarea.scrollHeight))}px`;
  }, [composer]);

  const visibleTasks = useMemo(() => {
    const query = search.trim().toLowerCase();
    const available = selectedTask?.id.startsWith('draft:') ? [selectedTask, ...tasks] : tasks;
    return query ? available.filter((task) => `${task.name} ${task.cwd} ${task.status}`.toLowerCase().includes(query)) : available;
  }, [search, selectedTask, tasks]);
  const conversation = useMemo(() => buildConversation(events), [events]);
  const projects = useMemo(() => groupTasksByProject(visibleTasks), [visibleTasks]);
  const activeApproval = selectedTask?.pendingApprovals?.find((approval) => approval.active);
  const selectedComposerMode = selectedTask ? composerMode(selectedTask) : 'send';
  const draftSelected = Boolean(selectedTask?.id.startsWith('draft:'));
  const composerBlocked = busy || (selectedTask ? (!draftSelected && composerIsBlocked(selectedTask.status)) : true);
  const activeTaskCount = tasks.filter((task) => !terminalStatuses.has(task.status)).length;
  const workflowStarters = rdkWorkflows(snapshot?.boardId);
  const selectedModel = selectedTask?.model ?? '';
  const modelPickerValue = selectedModel.startsWith('drobotics/') ? selectedModel : '';
  const effectiveModel = resolveEffectiveModel(models, selectedModel);
  const currentModelHealth = resolveCurrentModelHealth(modelHealth, effectiveModel);
  const imageInputSupported = modelAcceptsImages(models, selectedModel);
  const selectedPermissionMode = selectedTask?.permissionMode ?? 'ask';
  const canChangeModel = Boolean(selectedTask && (draftSelected || selectedTask.status === 'idle' || terminalStatuses.has(selectedTask.status)) && !busy);
  const canChangePermissions = canChangeModel;
  const canStopTask = Boolean(selectedTask && ['starting', 'running', 'waiting', 'stopping'].includes(selectedTask.status));
  const latestConversationItem = conversation[conversation.length - 1];
  const activityStart = optimisticPrompt && optimisticPrompt.taskId === selectedTask?.id
    ? optimisticPrompt.time
    : latestConversationItem?.kind === 'user' ? latestConversationItem.time : selectedTask?.updatedAt;

  useEffect(() => {
    setDeploymentStatus(null);
    if (!boardId || !selectedTask?.deployment || !connection?.capabilities?.capabilities.includes('deployments.v1')) return;
    let cancelled = false;
    const refresh = async () => {
      const status = await api.deploymentStatus(boardId, selectedTask.id).catch(() => null);
      if (!cancelled) setDeploymentStatus(status);
    };
    void refresh();
    if (!terminalStatuses.has(selectedTask.status) && selectedTask.status !== 'idle') {
      const timer = window.setInterval(() => void refresh(), 5000);
      return () => {cancelled = true; window.clearInterval(timer);};
    }
    return () => {cancelled = true;};
  }, [boardId, selectedTask?.id, selectedTask?.status, selectedTask?.deployment?.reportPath, connection?.capabilities?.capabilities]);

  async function submitPrompt(event: FormEvent) {
    event.preventDefault();
    const prompt = composer.trim();
    if (!prompt || !selectedTask || !boardId || composerBlocked) return;
    setBusy(true);
    setError('');
    const submittedAt = new Date().toISOString();
    const sourceTaskId = selectedTask.id;
    const submittedImages = attachments;
    if (!draftSelected) setOptimisticPrompt({taskId: selectedTask.id, text: prompt, time: submittedAt, attachments: submittedImages});
    setSelectedTask((current) => current?.id === selectedTask.id ? {...current, status: 'running', updatedAt: submittedAt} : current);
    setComposer('');
    setAttachments([]);
    followsOutput.current = true;
    try {
      let nextTask: Task | undefined;
      if (draftSelected) nextTask = await api.startTask(boardId, {
        name: selectedTask.name === 'New task' ? '' : selectedTask.name,
        cwd: selectedTask.cwd,
        prompt,
        images: submittedImages,
        approve: false,
        model: selectedModel || undefined,
        permissionMode: selectedPermissionMode,
      });
      else if (editingMessage !== null) nextTask = await api.forkTask(boardId, {taskId: selectedTask.id, sequence: editingMessage, prompt, images: submittedImages, kind: 'edit', model: selectedModel});
      else if (selectedComposerMode === 'resume') nextTask = await api.resumeTask(boardId, selectedTask.id, prompt, submittedImages);
      else if (selectedComposerMode === 'restart') nextTask = await api.restartTask(boardId, selectedTask.id, prompt, submittedImages);
      else await api.sendPrompt(boardId, selectedTask.id, prompt, submittedImages);
      if (isCurrentTarget(boardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setEditingMessage(null);
      taskDrafts.current.delete(selectedTask.id);
      await refreshTasks();
      if (nextTask && isCurrentTarget(boardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) {
        selectedTaskId.current = nextTask.id;
        setOptimisticPrompt({taskId: nextTask.id, text: prompt, time: submittedAt, attachments: submittedImages});
        setSelectedTask(nextTask);
        setWatchRevision((revision) => revision + 1);
      }
    } catch (reason) {
      if (isCurrentTarget(boardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) {
        setComposer(prompt);
        setAttachments(submittedImages);
        setOptimisticPrompt(null);
        setSelectedTask(selectedTask);
        setError(String(reason));
      }
    } finally {
      setBusy(false);
    }
  }

  async function stopTask() {
    if (!selectedTask || !boardId) return;
    const sourceBoardId = boardId;
    const sourceTaskId = selectedTask.id;
    setBusy(true);
    setError('');
    try {
      await api.stopTask(boardId, selectedTask.id);
      await refreshTasks();
    } catch (reason) {
      if (isCurrentTarget(sourceBoardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  async function respond(approval: Approval, response: Record<string, unknown>) {
    if (!selectedTask || !boardId) return;
    const sourceBoardId = boardId;
    const sourceTaskId = selectedTask.id;
    setBusy(true);
    setError('');
    try {
      await api.respond(boardId, selectedTask.id, approval.id, response);
      await refreshTasks();
    } catch (reason) {
      if (isCurrentTarget(sourceBoardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setError(String(reason));
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

  function retryFailedTurn(item: AssistantConversationItem) {
    const index = conversation.findIndex((candidate) => candidate.key === item.key);
    const prompt = conversation.slice(0, index).reverse().find((candidate): candidate is UserConversationItem => candidate.kind === 'user');
    if (!prompt) return;
    editMessage(prompt);
  }

  async function changeModel(value: string) {
    if (!selectedTask || !boardId || (!draftSelected && selectedTask.status !== 'idle' && !terminalStatuses.has(selectedTask.status))) return;
    const [provider, ...rest] = value.split('/');
    const modelId = rest.join('/');
    if (!provider || !modelId) return;
    const nextModel = models.find((model) => model.provider === provider && model.id === modelId);
    modelHealthRequest.current += 1;
    setCheckingModel(false);
    setModelHealth(null);
    if (attachments.length > 0 && nextModel?.capabilities?.imageInput !== true) {
      setAttachments([]);
      setNotice(`${nextModel?.name || modelId} does not support image input. Attachments were removed.`);
    }
    if (draftSelected) {
      setSelectedTask({...selectedTask, model: value});
      return;
    }
    const sourceTaskId = selectedTask.id;
    setBusy(true);
    setError('');
    try {
      await api.setModel(boardId, selectedTask.id, provider, modelId);
      if (isCurrentTarget(boardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setSelectedTask({...selectedTask, model: value});
      if (activeBoardId.current === boardId) setTasks((current) => current.map((task) => task.id === selectedTask.id ? {...task, model: value} : task));
    } catch (reason) {
      if (isCurrentTarget(boardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  async function checkModelHealth() {
    if (!boardId || !effectiveModel || checkingModel || !connection?.capabilities?.capabilities.includes('models.health.v1')) return;
    const targetBoard = boardId;
    const request = ++modelHealthRequest.current;
    setCheckingModel(true);
    setError('');
    try {
      const result = await api.modelHealth(targetBoard, `${effectiveModel.provider}/${effectiveModel.id}`, Boolean(currentModelHealth));
      if (modelHealthRequest.current === request && activeBoardId.current === targetBoard) setModelHealth(result);
    } catch (reason) {
      if (modelHealthRequest.current === request && activeBoardId.current === targetBoard) setError(friendlyError(String(reason)));
    } finally {
      if (modelHealthRequest.current === request) setCheckingModel(false);
    }
  }

  async function changePermissionMode(mode: string) {
    if (!selectedTask || !boardId || !canChangePermissions) return;
    if (draftSelected) {
      setSelectedTask({...selectedTask, permissionMode: mode as Task['permissionMode']});
      return;
    }
    const sourceTaskId = selectedTask.id;
    setBusy(true);
    setError('');
    try {
      const task = await api.setPermissionMode(boardId, selectedTask.id, mode);
      if (isCurrentTarget(boardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setSelectedTask(task);
      if (activeBoardId.current === boardId) setTasks((current) => current.map((item) => item.id === task.id ? task : item));
    } catch (reason) {
      if (isCurrentTarget(boardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setError(String(reason));
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
    if (draftSelected) {
      setSelectedTask({...selectedTask, name, updatedAt: new Date().toISOString()});
      setRenamingTask(false);
      return;
    }
    const sourceTaskId = selectedTask.id;
    setBusy(true);
    setError('');
    try {
      const task = await api.renameTask(boardId, selectedTask.id, name);
      if (isCurrentTarget(boardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setSelectedTask(task);
      if (activeBoardId.current === boardId) setTasks((current) => current.map((item) => item.id === task.id ? task : item));
      if (isCurrentTarget(boardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setRenamingTask(false);
    } catch (reason) {
      if (isCurrentTarget(boardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  function openNewTask(cwd = '') {
    const now = new Date().toISOString();
    const id = `draft:${Date.now()}`;
    setRenamingTask(false);
    setOpenMenu('');
    setEvents([]);
    setOptimisticPrompt(null);
    selectedTaskId.current = id;
    setSelectedTask({
      id,
      name: 'New task',
      cwd: cwd || selectedTask?.cwd || '/root',
      status: 'draft',
      createdAt: now,
      updatedAt: now,
      lastSequence: 0,
      model: models[0] ? `${models[0].provider}/${models[0].id}` : '',
      permissionMode: 'ask',
    });
    window.requestAnimationFrame(() => composerRef.current?.focus());
  }

  function beginRename(task: Task) {
    selectTask(task);
    setRenameValue(task.name);
    setRenamingTask(true);
    setOpenMenu('');
  }

  async function createSideTask(prompt: string, name: string) {
    if (!selectedTask || !boardId) return;
    const sourceBoardId = boardId;
    const sourceTaskId = selectedTask.id;
    setBusy(true);
    setError('');
    try {
      const task = await api.forkTask(boardId, {taskId: selectedTask.id, prompt, name, kind: 'side', model: selectedModel});
      await refreshTasks();
      if (!isCurrentTarget(sourceBoardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) return;
      selectedTaskId.current = task.id;
      setSelectedTask(task);
      setOptimisticPrompt({taskId: task.id, text: prompt, time: new Date().toISOString(), attachments: []});
      setShowSideTask(false);
    } catch (reason) {
      if (isCurrentTarget(sourceBoardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  async function startDeployment(request: StartDeploymentRequest) {
    if (!boardId) return;
    const sourceBoardId = boardId;
    const sourceTaskId = selectedTask?.id ?? '';
    setBusy(true);
    setError('');
    try {
      const task = await api.startDeployment(boardId, request);
      await refreshTasks();
      if (!isCurrentTarget(sourceBoardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) return;
      selectedTaskId.current = task.id;
      setSelectedTask(task);
      setShowDeployment(false);
      setWatchRevision((revision) => revision + 1);
    } catch (reason) {
      if (isCurrentTarget(sourceBoardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  async function refreshWorkspace() {
    if (!boardId || refreshing) return;
    setRefreshing(true);
    setConnectionState('connecting');
    setError('');
    try {
      const nextConnection = await api.refreshBoard(boardId);
      const [pageModels, nextSnapshot] = await Promise.all([
        api.models(boardId).catch(() => null),
        nextConnection.snapshot ? Promise.resolve(nextConnection.snapshot) : nextConnection.capabilities?.capabilities.includes('system.snapshot') ? api.systemSnapshot(boardId).catch(() => null) : Promise.resolve(null),
      ]);
      if (activeBoardId.current !== boardId) return;
      setConnection(nextConnection);
      setConnectionState('online');
      if (pageModels) setModels(pageModels);
      setSnapshot(nextSnapshot);
      await refreshTasks(boardId);
      setWatchRevision((revision) => revision + 1);
    } catch (reason) {
      if (activeBoardId.current === boardId) {
        setConnectionState('offline');
        setError(String(reason));
      }
    } finally {
      setRefreshing(false);
    }
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
    if (selectedTask?.id.startsWith('draft:') && selectedTask.id !== task.id) {
      taskDrafts.current.delete(selectedTask.id);
    }
    selectedTaskId.current = task.id;
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

  async function saveSupportBundle() {
    if (!boardId || busy) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const bundle = await api.saveSupportBundle(boardId);
      if (bundle.path) setNotice(`Support bundle saved · ${bundle.checks.pass} passed, ${bundle.checks.warn} warnings, ${bundle.checks.fail} failed · ${bundle.path}`);
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={`studio-shell ${showInspector ? '' : 'inspector-hidden'} ${isMacOS ? 'platform-macos' : ''}`}>
      <header className="titlebar" onDoubleClick={(event) => { if (shouldToggleMaximise(event.nativeEvent)) window.runtime?.WindowToggleMaximise?.(); }}>
        <div className="brand-lockup"><div className="brand-mark" aria-label="Hobot Code">H</div><span>Hobot Code</span></div>
        <button className="board-switcher" onClick={() => setShowBoard(true)} disabled={busy}>
          <span className={`connection-dot ${connectionState}`} />
          <span className="board-name">{connection?.board.name ?? 'Connect board'}</span>
          <ChevronDown size={14} />
        </button>
        <div className="titlebar-spacer" />
        {isMock() && <span className="preview-label">Preview</span>}
        {connection?.capabilities?.capabilities.includes('support.bundle.v1') && <button className="icon-button" title="Save private support bundle" disabled={busy || connectionState !== 'online'} onClick={() => void saveSupportBundle()}><Download size={16} /></button>}
        <button className="icon-button" title={connectionState === 'offline' ? 'Reconnect board' : 'Sync board now'} disabled={refreshing || !connection} onClick={() => void refreshWorkspace()}><RefreshCw size={16} className={refreshing ? 'spin' : ''} /></button>
        <button className={`icon-button ${showInspector ? 'active' : ''}`} title="Board monitor" onClick={() => setShowInspector((value) => !value)}><PanelRight size={17} /></button>
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
              <button className="project-toggle" onClick={() => toggleProject(project.path)} title={collapsedProjects.has(project.path) ? 'Expand project' : 'Collapse project'}><ChevronRight className="project-chevron" size={13} /><Folder size={14} /><span>{project.name}</span><small>{arrangeTasks(project.tasks).length}</small></button>
              <button className="row-more project-add" title={`New conversation in ${project.name}`} onClick={() => openNewTask(project.path)}><Plus size={14} /></button>
              <button className="row-more" title="Project actions" onClick={() => setOpenMenu((current) => current === `project:${project.path}` ? '' : `project:${project.path}`)}><MoreHorizontal size={15} /></button>
              {openMenu === `project:${project.path}` && <div className="row-menu"><button className="destructive" onClick={() => setDeleteTarget({kind: 'project', label: project.name, taskIds: project.tasks.map((task) => task.id)})}><Trash2 size={14} />Remove project</button></div>}
            </div>
            {!collapsedProjects.has(project.path) && <div className="project-conversations">{arrangeTasks(project.tasks).map(({task, depth, branchKind}) => <div key={task.id} className={`task-row-shell ${depth ? 'branch-task' : ''}`} style={{'--task-depth': depth} as any}>
              <button className={`task-row ${selectedTask?.id === task.id ? 'selected' : ''}`} onClick={() => {selectTask(task); setOpenMenu('');}}>
                {depth ? <CornerDownRight className="branch-mark" size={13} /> : <span className={`task-state-dot dot-${task.status}`} />}
                <span className="task-row-main"><span className="task-row-name">{task.name}</span>{depth > 0 && <span className="task-row-path">{branchKind === 'side' ? 'Side Agent' : 'Branch'}</span>}</span>
                <span className="task-row-time">{relativeTime(task.updatedAt)}</span>
              </button>
              <button className="row-more task-more" title="Conversation actions" onClick={() => setOpenMenu((current) => current === `task:${task.id}` ? '' : `task:${task.id}`)}><MoreHorizontal size={15} /></button>
              {openMenu === `task:${task.id}` && <div className="row-menu task-menu"><button onClick={() => beginRename(task)}><FilePenLine size={14} />Rename conversation</button>{!task.id.startsWith('draft:') && <button className="destructive" onClick={() => setDeleteTarget({kind: 'conversation', label: task.name, taskIds: [task.id]})}><Trash2 size={14} />Delete conversation</button>}</div>}
            </div>)}</div>}
          </section>)}
          {visibleTasks.length === 0 && <div className="empty-state"><ListTodo size={21} /><span>No tasks</span></div>}
        </div>
        {connection && <button className="board-summary" onClick={() => setShowBoard(true)}>
          <Server size={16} /><span><strong>{connection.board.name}</strong><small>{connection.daemon?.activeTasks ?? activeTaskCount}/{connection.daemon?.maximumTasks ?? 0} active</small></span><ChevronRight size={15} />
        </button>}
      </aside>

      <main className="task-main">
        {selectedTask ? <>
          <div className="task-header">
            <div className="task-title-block"><div className="task-title-line">{renamingTask ? <form className="title-editor" onSubmit={(event) => {event.preventDefault(); void renameSelectedTask();}}><input value={renameValue} maxLength={64} autoFocus onChange={(event) => setRenameValue(event.target.value)} onBlur={() => void renameSelectedTask()} onKeyDown={(event) => {if (event.key === 'Escape') {setRenameValue(selectedTask.name); setRenamingTask(false);}}} /></form> : <><h1 title="Double-click to rename" onDoubleClick={() => beginRename(selectedTask)}>{selectedTask.name}</h1><button className="title-edit" title="Rename conversation" onClick={() => beginRename(selectedTask)}><FilePenLine size={13} /></button></>}<span className={`status status-${selectedTask.status}`}>{statusLabel[selectedTask.status] ?? selectedTask.status}</span>{watchStatus && <span className={`stream-status stream-${watchStatus.state}`} role="status" title={watchStatus.message}><RefreshCw size={11} className="spin" />{watchStatusLabel(watchStatus)}</span>}</div><span className="workspace-path">{selectedTask.cwd}</span></div>
            <div className="task-actions">
              {!draftSelected && <button className="secondary-button side-task-button" title={selectedTask.sessionFile ? 'Create an independent agent from this conversation' : 'Side Agent is available after the first response'} onClick={() => setShowSideTask(true)} disabled={busy || !selectedTask.sessionFile}><GitBranch size={15} />Side Agent</button>}
              {terminalStatuses.has(selectedTask.status) && <button className="secondary-button" onClick={() => composerRef.current?.focus()}><RefreshCw size={14} />{selectedComposerMode === 'resume' ? 'Resume' : 'New session'}</button>}
            </div>
          </div>

          <div className="conversation" ref={timelineRef} onScroll={onTimelineScroll} aria-label="Conversation">
            <div className="conversation-inner">
              {eventsLoading && events.length === 0 && <div className="loading-conversation"><LoaderCircle size={18} className="spin" /><span>Loading conversation</span></div>}
              {!eventsLoading && conversation.length === 0 && <div className="empty-conversation"><div className="empty-symbol"><MessageSquare size={22} /></div><strong>{draftSelected ? 'What would you like to work on?' : 'Start a conversation'}</strong><div className="workflow-starters">{workflowStarters.map((workflow) => <button key={workflow.id} type="button" onClick={() => {if (workflow.id === 'deploy-model' && connection?.capabilities?.capabilities.includes('deployments.v1')) {setShowDeployment(true); return;} setComposer(workflow.prompt); window.requestAnimationFrame(() => composerRef.current?.focus());}}>{workflow.title}<ChevronRight size={13} /></button>)}</div></div>}
              {conversation.map((item) => item.kind === 'user'
                ? <UserMessage key={item.key} item={item} onEdit={editMessage} />
                : <AssistantTurn key={item.key} item={item} running={selectedTask.status === 'running' && !optimisticPrompt && item === conversation[conversation.length - 1]} canCheckModel={Boolean(effectiveModel && connection?.capabilities?.capabilities.includes('models.health.v1'))} checkingModel={checkingModel} onCheckModel={() => void checkModelHealth()} onRetry={() => retryFailedTurn(item)} />)}
              {optimisticPrompt?.taskId === selectedTask.id && !events.some((entry) => entry.normalized?.type === 'user.message' && String(entry.normalized.data?.text ?? '') === optimisticPrompt.text) && <UserMessage item={{kind: 'user', key: 'optimistic', sequence: Number.MAX_SAFE_INTEGER, time: optimisticPrompt.time, text: optimisticPrompt.text, attachments: optimisticPrompt.attachments.map((image) => ({name: image.name, mimeType: image.mimeType, preview: imageDataURL(image)}))}} />}
              {['starting', 'running'].includes(selectedTask.status) && <AgentProgress startedAt={activityStart} now={activityClock} hasOutput={!optimisticPrompt && latestConversationItem?.kind === 'assistant' && Boolean(latestConversationItem.text || latestConversationItem.thinking || latestConversationItem.tools.length)} />}
            </div>
          </div>

          {hasNewOutput && <button className="jump-latest" onClick={scrollToLatest}><ArrowDown size={15} />New output</button>}
          <div className="composer-dock">
            {activeApproval && <ApprovalBar key={activeApproval.id} approval={activeApproval} busy={busy} respond={(response) => respond(activeApproval, response)} />}
            <form className="composer" onSubmit={submitPrompt}>
              {editingMessage !== null && <div className="editing-banner"><FilePenLine size={14} /><span>Editing this message. Later messages will be replaced.</span><button type="button" title="Cancel edit" onClick={() => {setEditingMessage(null); setComposer('');}}><X size={14} /></button></div>}
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
                <ImagePickerButton disabled={composerBlocked || attachments.length >= 4 || !imageInputSupported} title={imageInputSupported ? 'Attach images' : `${effectiveModel?.name || 'The selected model'} does not support image input`} onPick={(images) => {try {setAttachments(appendImages(attachments, images));} catch (reason) {setError(friendlyError(String(reason)));}}} onError={setError} />
                <label className="model-picker" title={canChangeModel ? 'Choose model' : 'Stop the current turn before changing models'}><select aria-label="Model" value={modelPickerValue} disabled={!canChangeModel} onChange={(event) => void changeModel(event.target.value)}><option value="" disabled>Board default</option>{selectedModel.startsWith('drobotics/') && !models.some((model) => `${model.provider}/${model.id}` === selectedModel) && <option value={selectedModel}>{selectedModel.split('/').at(-1)}</option>}{models.map((model) => <option key={`${model.provider}/${model.id}`} value={`${model.provider}/${model.id}`}>{model.name || model.id}</option>)}</select><ChevronDown size={12} /></label>
                {connection?.capabilities?.capabilities.includes('models.health.v1') && <button className={`model-health-button ${currentModelHealth?.status ?? ''}`} type="button" title={currentModelHealth ? `${currentModelHealth.message} Click to check again.${currentModelHealth.cached ? ' Cached result.' : ''}` : 'Check model availability'} onClick={() => void checkModelHealth()} disabled={checkingModel || !effectiveModel}>{checkingModel ? <LoaderCircle size={12} className="spin" /> : currentModelHealth?.status === 'available' ? <Check size={12} /> : currentModelHealth?.status === 'unavailable' ? <XCircle size={12} /> : <Activity size={12} />}<span>{checkingModel ? 'Checking' : currentModelHealth?.status === 'available' ? `${currentModelHealth.latencyMs ?? 0} ms` : currentModelHealth?.status === 'unavailable' ? modelHealthLabel(currentModelHealth.category) : 'Check'}</span></button>}
                <label className="permission-picker" title={canChangePermissions ? 'Choose approval mode' : 'Stop the current turn before changing permissions'}><ShieldCheck size={13} /><select aria-label="Approval mode" value={selectedPermissionMode} disabled={!canChangePermissions} onChange={(event) => void changePermissionMode(event.target.value)}><option value="review">Review only</option><option value="ask">Ask for changes</option><option value="developer">Developer</option></select><ChevronDown size={12} /></label>
                <span className="composer-state">{draftSelected ? 'Starts when sent' : editingMessage !== null ? 'Replaces later messages' : selectedComposerMode === 'resume' ? 'Resume session' : selectedComposerMode === 'restart' ? 'New session' : statusLabel[selectedTask.status] ?? selectedTask.status}</span>
                {canStopTask ? <button className="send-button stop-mode" type="button" title="Stop" onClick={stopTask} disabled={busy || selectedTask.status === 'stopping'}><Square size={14} fill="currentColor" /></button> : <button className="send-button" type="submit" title="Send" disabled={!composer.trim() || composerBlocked}><ArrowUp size={17} /></button>}
              </div>
            </form>
          </div>
        </> : <div className="main-empty"><div className="empty-symbol"><Bot size={24} /></div><strong>Select a task</strong><span>Choose an existing conversation or create a new one.</span></div>}
      </main>

      {showInspector && <aside className="inspector">
        <div className="inspector-header"><span>Board monitor</span><div className="inspector-header-actions"><small>{snapshot ? `Updated ${relativeTime(snapshot.capturedAt)}` : 'Not sampled'}</small><button className="icon-button compact" title="Close monitor" onClick={() => setShowInspector(false)}><X size={16} /></button></div></div>
        {connection && <BoardMonitor connection={connection} connectionState={connectionState} snapshot={snapshot} task={selectedTask} />}
        {selectedTask?.deployment && <DeploymentInspector status={deploymentStatus} record={selectedTask.deployment} />}
      </aside>}

      {error && <div className="error-toast"><XCircle size={17} /><span>{friendlyError(error)}</span><button title="Dismiss" onClick={() => setError('')}><X size={15} /></button></div>}
      {notice && <div className="success-toast"><Check size={17} /><span>{notice}</span><button title="Dismiss" onClick={() => setNotice('')}><X size={15} /></button></div>}
      {showBoard && <BoardDialog boards={boards} busy={busy} onClose={() => boards.length > 0 && setShowBoard(false)} onConnect={connect} onSave={async (board) => {setBusy(true); setError(''); try {const saved = await api.saveBoard(board); setBoards(await api.listBoards()); await connect(saved);} catch (reason) {setError(String(reason));} finally {setBusy(false);}}} />}
      {showSideTask && selectedTask && <SideTaskDialog parent={selectedTask} busy={busy} onClose={() => setShowSideTask(false)} onCreate={createSideTask} />}
      {showDeployment && selectedTask && snapshot && <DeploymentDialog boardId={boardId} cwd={selectedTask.cwd} snapshot={snapshot} models={models} busy={busy} onClose={() => setShowDeployment(false)} onStart={startDeployment} />}
      {deleteTarget && <DeleteDialog target={deleteTarget} busy={busy} onClose={() => setDeleteTarget(null)} onDelete={confirmDelete} />}
    </div>
  );
}

function UserMessage({item, onEdit}: {item: UserConversationItem; onEdit?: (item: UserConversationItem) => void}) {
  return <article className="user-message">{item.attachments.length > 0 && <div className="message-attachments">{item.attachments.map((attachment, index) => attachment.preview ? <img key={`${attachment.name}-${index}`} src={attachment.preview} alt={attachment.name || `Attached image ${index + 1}`} /> : <span key={`${attachment.name}-${index}`}><Paperclip size={12} />{attachment.name || attachment.mimeType}</span>)}</div>}<div className="user-message-content">{item.text}</div><div className="message-actions"><time>{formatTime(item.time)}</time><CopyButton value={item.text} />{onEdit && <button className="copy-button" title="Edit from this point" onClick={() => onEdit(item)}><FilePenLine size={14} /></button>}</div></article>;
}

function AssistantTurn({item, running, canCheckModel, checkingModel, onCheckModel, onRetry}: {item: AssistantConversationItem; running: boolean; canCheckModel: boolean; checkingModel: boolean; onCheckModel: () => void; onRetry: () => void}) {
  return <article className="assistant-turn">
    {item.thinking && <ThinkingBlock item={item} running={running} />}
    {item.tools.length > 0 && <ToolGroup tools={item.tools} />}
    {item.notices.map((notice, index) => <div key={`${notice.time}-${index}`} className={`turn-notice notice-${notice.type}`}><Activity size={13} /><span>{notice.label}</span></div>)}
    {item.text && <div className="assistant-content"><MarkdownContent value={item.text} /><div className="assistant-actions"><CopyButton value={item.text} /></div></div>}
    {item.failure && <section className="turn-failure" role="alert"><div className="turn-failure-heading"><AlertTriangle size={16} /><strong>{item.failure.title}</strong></div><p>{item.failure.message}</p><div className="turn-failure-actions">{canCheckModel && <button className="secondary-button" type="button" onClick={onCheckModel} disabled={checkingModel}>{checkingModel ? <LoaderCircle size={14} className="spin" /> : <Activity size={14} />}Check model</button>}<button className="secondary-button" type="button" onClick={onRetry}><RefreshCw size={14} />Edit and retry</button></div></section>}
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
  return <div className="markdown"><ReactMarkdown skipHtml remarkPlugins={markdownRemarkPlugins} components={{
    a: ({node: _node, href, ...props}: any) => <a {...props} href={href} onClick={(event) => {
      event.preventDefault();
      if (href) void api.openExternalURL(href);
    }} />,
  }}>{value}</ReactMarkdown></div>;
}

function ImagePickerButton({disabled, title, onPick, onError}: {disabled: boolean; title: string; onPick: (images: ImageContent[]) => void; onError: (message: string) => void}) {
  const input = useRef<HTMLInputElement>(null);
  return <><button className="icon-button compact attach-button" type="button" title={title} disabled={disabled} onClick={() => input.current?.click()}><Paperclip size={15} /></button><input ref={input} className="file-input" type="file" accept="image/jpeg,image/png,image/webp,image/gif" multiple onChange={(event) => {const files = [...(event.target.files ?? [])]; event.target.value = ''; void Promise.all(files.map(prepareImage)).then(onPick).catch((reason) => onError(friendlyError(String(reason))));}} /></>;
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
  const [value, setValue] = useState(approval.prefill ?? '');
  const textRequest = approval.method === 'input' || approval.method === 'editor';
  return <form className="approval-bar" role="alert" aria-label={view.title} onSubmit={(event) => {event.preventDefault(); if (textRequest) respond(approvalResponse(approval.method, 'submit', value));}}><div className="approval-heading"><div className="approval-icon"><ShieldCheck size={17} /></div><strong>{view.title}</strong></div><pre className="approval-detail">{view.detail}</pre>{view.remembersExactCall && <div className="approval-scope"><ShieldCheck size={13} />Remembering applies only to this exact tool call in this task.</div>}{approval.method === 'input' && <input className="approval-input" value={value} placeholder={approval.placeholder} autoFocus onChange={(event) => setValue(event.target.value)} disabled={busy} />}{approval.method === 'editor' && <textarea className="approval-input approval-editor" value={value} placeholder={approval.placeholder} rows={5} autoFocus onChange={(event) => setValue(event.target.value)} disabled={busy} />}<div className="approval-actions">{approval.method === 'select' && <>{approval.options?.map((option) => <button type="button" key={option} className={option === 'Allow once' ? 'primary-button' : 'secondary-button'} disabled={busy} onClick={() => respond(approvalResponse(approval.method, 'select', option))}>{option}</button>)}<button type="button" className="secondary-button" disabled={busy} onClick={() => respond(approvalResponse(approval.method, 'cancel'))}>Cancel</button></>}{approval.method === 'confirm' && <><button type="button" className="secondary-button" disabled={busy} onClick={() => respond(approvalResponse(approval.method, 'deny'))}>Deny</button><button type="button" className="primary-button" disabled={busy} onClick={() => respond(approvalResponse(approval.method, 'confirm'))}>Allow once</button></>}{textRequest && <><button type="button" className="secondary-button" disabled={busy} onClick={() => respond(approvalResponse(approval.method, 'cancel'))}>Cancel</button><button className="primary-button" type="submit" disabled={busy}>Submit</button></>}</div></form>;
}

function BoardDialog({boards, busy, onClose, onConnect, onSave}: {boards: Board[]; busy: boolean; onClose: () => void; onConnect: (board: Board) => void; onSave: (board: Board) => Promise<void>}) {
  const [editing, setEditing] = useState(boards.length === 0);
  const [form, setForm] = useState<Board>({id: '', name: 'RDK S100', host: '', user: 'root', port: 22, identityFile: ''});
  return <div className="modal-backdrop"><div className="modal board-modal"><div className="modal-header"><div><span className="modal-eyebrow">Boards</span><h2>{editing ? 'Add board' : 'Connect'}</h2></div>{boards.length > 0 && <button className="icon-button" title="Close" onClick={onClose}><X size={18} /></button>}</div>{!editing ? <><div className="saved-boards">{boards.map((board) => <button key={board.id} className="saved-board" onClick={() => onConnect(board)} disabled={busy}><Server size={19} /><span><strong>{board.name}</strong><small>{board.user}@{board.host}:{board.port}</small></span><ChevronRight size={15} /></button>)}</div><button className="add-board-row" onClick={() => setEditing(true)}><Plus size={16} />Add board</button></> : <form onSubmit={(event) => {event.preventDefault(); void onSave(form);}} className="form-grid"><div className="board-presets">{boardPresets.map((preset) => <button type="button" key={preset.name} className={form.name === preset.name ? 'selected' : ''} onClick={() => setForm({...form, ...preset})}><Server size={14} /><span>{preset.name}</span></button>)}</div><label><span>Name</span><input value={form.name} onChange={(event) => setForm({...form, name: event.target.value})} required /></label><label><span>Host</span><input value={form.host} onChange={(event) => setForm({...form, host: event.target.value})} placeholder="Board IP or hostname" autoFocus required /></label><div className="form-row"><label><span>User</span><input value={form.user} onChange={(event) => setForm({...form, user: event.target.value})} required /></label><label><span>Port</span><input type="number" min="1" max="65535" value={form.port} onChange={(event) => setForm({...form, port: Number(event.target.value)})} required /></label></div><label><span>Identity file</span><input value={form.identityFile} onChange={(event) => setForm({...form, identityFile: event.target.value})} placeholder="Use SSH agent or config" /></label><div className="modal-actions">{boards.length > 0 && <button type="button" className="secondary-button" onClick={() => setEditing(false)}>Back</button>}<button className="primary-button" type="submit" disabled={busy}>{busy ? <LoaderCircle size={15} className="spin" /> : <Server size={15} />}Save & connect</button></div></form>}</div></div>;
}

function SideTaskDialog({parent, busy, onClose, onCreate}: {parent: Task; busy: boolean; onClose: () => void; onCreate: (prompt: string, name: string) => void}) {
  const [prompt, setPrompt] = useState('');
  const [name, setName] = useState('');
  return <div className="modal-backdrop"><form className="modal side-task-modal" onSubmit={(event) => {event.preventDefault(); onCreate(prompt.trim(), name.trim());}}><div className="modal-header"><div><span className="modal-eyebrow">Parallel branch</span><h2>New side task</h2></div><button type="button" className="icon-button" title="Close" onClick={onClose}><X size={18} /></button></div><div className="fork-source"><GitBranch size={16} /><span><strong>Shares the settled context from {parent.name}</strong><small>The main task stays unchanged and both can continue independently.</small></span></div><div className="form-grid"><label><span>Name</span><input value={name} onChange={(event) => setName(event.target.value)} placeholder={`${parent.name}-side`} /></label><label><span>Instruction</span><textarea rows={6} value={prompt} onChange={(event) => setPrompt(event.target.value)} autoFocus required /></label><div className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>Cancel</button><button className="primary-button" disabled={busy || !prompt.trim()}><GitBranch size={15} />Start side task</button></div></div></form></div>;
}

function DeploymentDialog({boardId, cwd, snapshot, models, busy, onClose, onStart}: {boardId: string; cwd: string; snapshot: SystemSnapshot; models: ModelOption[]; busy: boolean; onClose: () => void; onStart: (request: StartDeploymentRequest) => void}) {
  const [inspection, setInspection] = useState<DeploymentInspection | null>(null);
  const [loading, setLoading] = useState(true);
  const [failure, setFailure] = useState('');
  const [artifactPath, setArtifactPath] = useState('');
  const [goal, setGoal] = useState<StartDeploymentRequest['goal']>('deploy-and-validate');
  const [model, setModel] = useState(models[0] ? `${models[0].provider}/${models[0].id}` : '');
  const [permissionMode, setPermissionMode] = useState('ask');

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api.inspectDeployment(boardId, cwd).then((result) => {
      if (cancelled) return;
      setInspection(result);
      const preferred = preferredDeploymentArtifact(result.artifacts);
      setArtifactPath(preferred?.path ?? '');
      setFailure('');
    }).catch((reason) => !cancelled && setFailure(friendlyError(String(reason)))).finally(() => !cancelled && setLoading(false));
    return () => {cancelled = true;};
  }, [boardId, cwd]);

  const selected = inspection?.artifacts.find((artifact) => artifact.path === artifactPath);
  const profile = deploymentProfileFor(selected, snapshot.boardId);
  const canStart = deploymentCanStart(selected) && !loading && !busy;
  return <div className="modal-backdrop"><form className="modal deployment-modal" onSubmit={(event) => {event.preventDefault(); if (canStart && selected) onStart({cwd, artifactPath: selected.path, goal, name: `Deploy ${selected.name}`, model: model || undefined, permissionMode, profile: profile || undefined});}}><div className="modal-header"><div><span className="modal-eyebrow">RDK deployment</span><h2>Deploy and validate a model</h2></div><button type="button" className="icon-button" title="Close" onClick={onClose}><X size={18} /></button></div><div className="deployment-target"><Cpu size={17} /><span><strong>{snapshot.board}</strong><small>{snapshot.boardId.toUpperCase()} · RDK OS {snapshot.rdkOsVersion} · {cwd}</small></span></div>{loading ? <div className="deployment-loading"><LoaderCircle size={17} className="spin" />Scanning model artifacts</div> : failure ? <div className="deployment-error"><AlertTriangle size={15} />{failure}</div> : <div className="form-grid"><label><span>Model artifact</span><select value={artifactPath} onChange={(event) => setArtifactPath(event.target.value)} required><option value="" disabled>{inspection?.artifacts.length ? 'Choose an artifact' : 'No supported artifacts found'}</option>{inspection?.artifacts.map((artifact) => <option key={artifact.path} value={artifact.path} disabled={artifact.compatibility === 'mismatch'}>{artifact.relativePath} · {artifact.kind} · {deploymentCompatibilityLabel(artifact.compatibility)}</option>)}</select></label>{selected && <div className={`artifact-assessment assessment-${selected.compatibility}`}><div><Box size={15} /><strong>{deploymentCompatibilityLabel(selected.compatibility)}</strong><span>{formatBytes(selected.sizeBytes)}</span></div><p>{selected.reason}</p>{profile && <p>Frozen acceptance: {profile}</p>}</div>}<label><span>Goal</span><select value={goal} onChange={(event) => setGoal(event.target.value as StartDeploymentRequest['goal'])}><option value="deploy-and-validate">Deploy, verify, and benchmark</option><option value="benchmark">Validate an existing artifact</option></select></label><div className="form-row deployment-options"><label><span>Agent model</span><select value={model} onChange={(event) => setModel(event.target.value)}>{models.map((option) => <option key={`${option.provider}/${option.id}`} value={`${option.provider}/${option.id}`}>{option.name || option.id}</option>)}</select></label><label><span>Permissions</span><select value={permissionMode} onChange={(event) => setPermissionMode(event.target.value)}><option value="ask">Ask</option><option value="developer">Developer</option></select></label></div>{inspection?.truncated && <div className="deployment-note"><AlertTriangle size={13} />Scan limit reached. Narrow the project directory if the artifact is missing.</div>}<div className="deployment-note"><ShieldCheck size={13} />Commands and file changes remain subject to the board-side permission policy. Completion requires a verified report and artifact digest.</div><div className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>Cancel</button><button className="primary-button" disabled={!canStart}>{busy ? <LoaderCircle size={15} className="spin" /> : <Gauge size={15} />}Start deployment</button></div></div>}</form></div>;
}

function DeploymentInspector({status, record}: {status: DeploymentStatus | null; record: NonNullable<Task['deployment']>}) {
  const phase = status?.phase ?? 'checking';
  const report = status?.report;
  const metrics = report?.correctness.metrics ?? [];
  const peak = report?.resources?.peak;
  return <InspectorSection title="Model deployment"><div className={`deployment-phase phase-${phase}`}><span>{deploymentPhaseLabel(phase)}</span>{phase === 'running' || phase === 'checking' ? <LoaderCircle size={13} className="spin" /> : phase === 'passed' ? <Check size={13} /> : <AlertTriangle size={13} />}</div><InfoRow label="Target" value={`${record.boardId.toUpperCase()} · ${record.rdkOsVersion || 'unknown OS'}`} /><InfoRow label="Goal" value={record.goal} />{record.acceptance?.profile && <InfoRow label="Acceptance" value={record.acceptance.profile} />}<InfoRow label="Artifact" value={record.artifact.name} mono copy={record.artifact.path} />{report && <><InfoRow label="Correctness" value={report.correctness.passed ? 'Passed' : 'Not passed'} />{report.correctness.dataset && <InfoRow label="Dataset" value={`${report.correctness.dataset}${report.correctness.sampleCount ? ` · ${report.correctness.sampleCount} samples` : ''}`} />}{metrics.map((metric) => <InfoRow key={metric.name} label={metric.name} value={`${metric.value.toFixed(4)} ${metric.unit} · ${metric.comparator} ${metric.threshold}`} />)}<InfoRow label="Measured" value={report.performance.iterations ? `${report.performance.iterations} iterations` : 'No benchmark'} />{report.performance.p50LatencyMs ? <InfoRow label="Model latency" value={`p50 ${report.performance.p50LatencyMs.toFixed(2)} ms · p95 ${(report.performance.p95LatencyMs ?? 0).toFixed(2)} ms`} /> : null}{report.performance.endToEndP50Ms ? <InfoRow label="End-to-end" value={`p50 ${report.performance.endToEndP50Ms.toFixed(2)} ms · p95 ${(report.performance.endToEndP95Ms ?? 0).toFixed(2)} ms`} /> : null}{peak?.bpuUtilizationPercent ? <InfoRow label="Peak BPU" value={`${peak.bpuUtilizationPercent.toFixed(1)}%`} /> : null}{peak?.maxTemperatureC ? <InfoRow label="Peak temperature" value={`${peak.maxTemperatureC.toFixed(1)} C`} /> : null}{peak?.systemMemoryAvailableBytes ? <InfoRow label="Memory at peak" value={`${formatBytes(peak.systemMemoryAvailableBytes)} available${peak.aiAllocatedBytes ? ` · ${formatBytes(peak.aiAllocatedBytes)} ${(peak.aiAllocationSource || 'AI').toUpperCase()}` : peak.ionAllocatedBytes ? ` · ${formatBytes(peak.ionAllocatedBytes)} ION` : ''}`} /> : null}<div className="deployment-summary">{report.summary}</div></>}{status?.issue && <div className="deployment-issue"><AlertTriangle size={13} />{status.issue}</div>}<details className="deployment-report-path"><summary>Report path<ChevronRight size={12} /></summary><div>{record.reportPath}<CopyButton value={record.reportPath} /></div></details></InspectorSection>;
}

function DeleteDialog({target, busy, onClose, onDelete}: {target: {kind: 'conversation' | 'project'; label: string; taskIds: string[]}; busy: boolean; onClose: () => void; onDelete: () => void}) {
  const project = target.kind === 'project';
  return <div className="modal-backdrop"><div className="modal confirm-modal"><div className="confirm-icon"><Trash2 size={18} /></div><h2>{project ? 'Remove project?' : 'Delete conversation?'}</h2><p>{project ? `This removes ${target.taskIds.length} conversation${target.taskIds.length === 1 ? '' : 's'} from ${target.label}.` : `This permanently removes ${target.label} from Hobot Code.`}</p><small>Running agents will stop. Files in the board workspace will not be deleted.</small><div className="modal-actions"><button className="secondary-button" onClick={onClose} disabled={busy}>Cancel</button><button className="danger-button" onClick={onDelete} disabled={busy}>{busy ? <LoaderCircle size={15} className="spin" /> : <Trash2 size={15} />}Delete</button></div></div></div>;
}

function BoardMonitor({connection, connectionState, snapshot, task}: {connection: Connection; connectionState: 'connecting' | 'online' | 'offline'; snapshot: SystemSnapshot | null; task: Task | null}) {
  if (!snapshot) {
    return <><CompatibilityPanel connection={connection} /><InspectorSection title="Board health"><div className="board-identity"><Server size={18} /><div><strong>{connection.board.name}</strong><span>{connection.board.user}@{connection.board.host} · {connectionState === 'online' ? 'Online' : connectionState === 'connecting' ? 'Checking' : 'Offline'}</span></div></div><div className="snapshot-empty">Hardware telemetry is unavailable on this board-side version. Tasks remain available.</div></InspectorSection></>;
  }
  const utilization = bpuUtilization(snapshot);
  const health = boardHealth(snapshot);
  const resources = systemResourceMetrics(snapshot);
  const memoryMetrics = acceleratorMemoryMetrics(snapshot);
  const orphanedION = orphanedIONNotice(snapshot.aiMemory?.ionOrphanedBytes ?? 0);
  const bandwidth = activeDDRBandwidth(snapshot);
  const acceleratorProcesses = snapshot.accelerator?.available ? snapshot.accelerator.processes ?? [] : [];
  const taskAlerts = [task?.lastError ? {tone: 'danger', label: task.lastError} : null, task?.logTruncated ? {tone: 'warning', label: 'Older task events were truncated.'} : null].filter(Boolean) as Array<{tone: string; label: string}>;
  const alerts = [...health.issues, ...taskAlerts];
  return <>
    <CompatibilityPanel connection={connection} />
    <section className="board-overview"><div className="board-identity"><Server size={18} /><div><strong>{snapshot.board}</strong><span>{snapshot.hostname} · {connectionState === 'online' ? 'Live' : connectionState === 'connecting' ? 'Checking' : 'Offline'}</span></div></div><div className="board-meta"><span>RDK OS {snapshot.rdkOsVersion || '-'}</span><span>Up {durationLabel(snapshot.uptimeSeconds)}</span></div></section>
    <InspectorSection title="BPU">
      <div className="bpu-hero">
        <div className="bpu-hero-heading"><span><Cpu size={15} />BPU load</span><strong>{utilization.available ? percentLabel(utilization.average) : 'Not reported'}</strong></div>
        <div className="bpu-hero-meta"><span>{bpuCoreLabel(snapshot)}</span>{utilization.available && <span>Peak core {utilization.peakCore} · {percentLabel(utilization.peak)}</span>}</div>
        {snapshot.bpuCores?.length ? <div className="bpu-core-list">{snapshot.bpuCores.map((core) => <div className="bpu-core" key={core.index}><span>{core.name}</span><div className="bpu-track"><i style={{width: `${Math.max(0, Math.min(100, core.utilizationPercent))}%`}} /></div><strong>{percentLabel(core.utilizationPercent)}</strong></div>)}</div> : <div className="bpu-unavailable">{bpuUnavailableReason(snapshot)}</div>}
      </div>
      <div className="accelerator-stats"><InfoRow label="Frequency" value={bpuFrequency(snapshot)} /><InfoRow label="Temperature" value={bpuTemperature(snapshot)} />{bandwidth && <InfoRow label="DDR bandwidth" value={`R ${bandwidth.read.toFixed(0)} · W ${bandwidth.write.toFixed(0)} MiB/s`} />}</div>
    </InspectorSection>
    <InspectorSection title="Resources"><div className="resource-list">{resources.map((metric) => <ResourceBar key={metric.key} label={metric.label} value={metric.value} percent={metric.percent} tone={metric.tone} />)}</div></InspectorSection>
    {snapshot.hardwareLeases?.length ? <InspectorSection title="Hardware in use"><div className="hardware-leases">{snapshot.hardwareLeases.map((lease) => <div className="hardware-lease" key={lease.resource}><Cpu size={13} /><span><strong>{hardwareResourceLabel(lease.resource)}</strong><small>{lease.taskId} · PID {lease.pid}</small></span></div>)}</div></InspectorSection> : null}
    <InspectorSection title="Hbmem">
      {memoryMetrics.length ? <><div className="resource-list compact">{memoryMetrics.map((metric) => <ResourceBar key={metric.key} label={metric.label} value={metric.value} detail={metric.detail} percent={metric.percent} available={metric.available} />)}</div>{!snapshot.accelerator?.available && <p className="memory-footnote">Upgrade the board service for reserved capacity and process attribution.</p>}{snapshot.accelerator?.source === 'hrt_ucp_monitor-estimate' && <p className="memory-footnote">Estimated by the board monitor; process ownership may be incomplete.</p>}{orphanedION && <div className={`memory-warning${orphanedION.warning ? '' : ' minor'}`}>{orphanedION.warning && <AlertTriangle size={13} />}{orphanedION.label}</div>}</> : <div className="snapshot-empty">Hbmem counters are not exposed by this RDK OS.</div>}
    </InspectorSection>
    {acceleratorProcesses.length > 0 && <InspectorSection title="Hbmem processes"><div className="accelerator-processes">{acceleratorProcesses.slice(0, 8).map((process) => <div className="accelerator-process" key={process.pid}><div><strong>{process.name}</strong><span>PID {process.pid}</span></div><div><strong>{formatBytes(process.hbmemBytes)} Hbmem</strong><span>{formatBytes(process.rssBytes)} RSS</span></div></div>)}</div></InspectorSection>}
    {alerts.length > 0 && <InspectorSection title="Attention"><div className="health-issues">{alerts.map((issue) => <div key={issue.label} className={issue.tone}><AlertTriangle size={14} /><span>{issue.label}</span></div>)}</div></InspectorSection>}
  </>;
}

function CompatibilityPanel({connection}: {connection: Connection}) {
  const compatibility = connection.compatibility;
  if (!compatibility) return null;
  const label = compatibility.status === 'supported' ? 'Supported' : compatibility.status === 'limited' ? 'Limited' : 'Upgrade required';
  return <InspectorSection title="Compatibility"><div className={`compatibility-summary compatibility-${compatibility.status}`}><span>{label}</span><strong>{compatibility.summary}</strong></div><InfoRow label="Studio / board" value={`${compatibility.appVersion} / ${compatibility.agentdVersion}`} /><InfoRow label="Protocol / events" value={`${compatibility.protocol} / ${compatibility.eventSchema}`} />{compatibility.boardId && <InfoRow label="Validated target" value={compatibility.validatedTarget ? `${compatibility.boardId.toUpperCase()} · ${compatibility.rdkOsVersion}` : 'Not fully validated'} />}{compatibility.issues.length > 0 && <div className="compatibility-issues">{compatibility.issues.map((issue) => <div key={issue.code} className={issue.severity}><AlertTriangle size={13} /><span><strong>{issue.message}</strong>{issue.action && <small>{issue.action}</small>}</span></div>)}</div>}</InspectorSection>;
}

function InspectorSection({title, children}: {title: string; children: ReactNode}) { return <section className="inspector-section"><h3>{title}</h3>{children}</section>; }
function InfoRow({label, value, mono, copy}: {label: string; value: string; mono?: boolean; copy?: string}) { return <div className="info-row"><span>{label}</span><div><strong className={mono ? 'mono' : ''}>{value}</strong>{copy && <CopyButton value={copy} />}</div></div>; }
function CopyButton({value}: {value: string}) { const [copied, setCopied] = useState(false); return <button type="button" className="copy-button" title={copied ? 'Copied' : 'Copy'} onClick={() => void navigator.clipboard.writeText(value).then(() => {setCopied(true); window.setTimeout(() => setCopied(false), 1200);})}>{copied ? <Check size={13} /> : <Clipboard size={13} />}</button>; }
function ResourceBar({label, value, detail, percent, available = false, tone = ''}: {label: string; value: string; detail?: string; percent?: number; available?: boolean; tone?: string}) {
  const bounded = Math.max(0, Math.min(100, percent ?? 0));
  return <div className={`resource-bar ${tone} ${available ? 'available' : ''}`}><div><span>{label}</span><strong>{value}</strong></div>{detail && <small>{detail}</small>}{percent !== undefined && <div className="resource-track" role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(bounded)}><i style={{width: `${bounded}%`}} /></div>}</div>;
}
function formatTime(value: string) { return new Date(value).toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'}); }
function hardwareResourceLabel(value: string) { if (value === 'bpu') return 'BPU'; if (value === 'media-pipeline') return 'Media pipeline'; if (value.startsWith('camera-video')) return value.replace('camera-', '/dev/'); return value; }
function relativeTime(value: string) { const seconds = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 1000)); if (seconds < 60) return 'now'; if (seconds < 3600) return `${Math.floor(seconds / 60)}m`; if (seconds < 86_400) return `${Math.floor(seconds / 3600)}h`; return `${Math.floor(seconds / 86_400)}d`; }
function friendlyError(value: string) {
  const message = value.replace(/^Error:\s*/i, '').replace(/^task_[a-z_]+:\s*/i, '');
  if (/board configuration changed|configuration-restart-required/i.test(message)) return 'The board configuration changed. Run `hobot daemon restart` on the board, then reconnect.';
  if (/context deadline exceeded|operation timed out|connect to host .* timed out/i.test(message)) return 'Could not reach the board. Check the network or VPN and try again.';
  if (/requires a newer Hobot Code event schema/i.test(message)) return 'Update the board-side Hobot Code and reconnect.';
  if (/has no resumable Hobot Code session/i.test(message)) return 'This task has no saved session. Start a new session instead.';
  if (/background task limit reached.*all agents are currently working/i.test(message)) return 'Both background slots are busy. Stop a working agent, or wait for one to finish.';
  return message;
}

export default App;
