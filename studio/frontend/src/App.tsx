import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import type {FormEvent, ReactNode, UIEvent} from 'react';
import ReactMarkdown from 'react-markdown';
import {
  Activity, AlertTriangle, ArrowDown, ArrowUp, Bot, Box, Brain, Check, ChevronDown,
  ChevronRight, Clipboard, CornerDownRight, Cpu, FilePenLine, Folder,
  GitBranch, ListTodo, LoaderCircle, MessageSquare,
  Download, FileDiff, Gauge, Info, MoreHorizontal, PanelRight, Paperclip, Plus, RefreshCw, Search, Server, ShieldCheck,
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
import {taskAttention} from './task-notifications.js';
import {taskRecovery, taskRecoveryActionAvailable} from './task-recovery.js';
import {compatibilityPresentation, compatibilityTargetLabel} from './compatibility-presentation.js';
import {workspaceChangeLabel, workspaceChangeSummary, workspaceDeliverySummary, workspaceDiffLines} from './workspace-changes.js';
import {extensionCatalogHealth, extensionCatalogSummary, extensionKindLabel, extensionTargetState, filterExtensions} from './extension-center.js';
import type {AssistantConversationItem, ToolActivity, UserConversationItem} from './conversation-model.js';
import type {Approval, Board, BoardUpdateCheck, BoardUpdateResult, Connection, DeploymentInspection, DeploymentStatus, ExtensionCatalog, ImageContent, ModelConformance, ModelHealth, ModelOption, StartDeploymentRequest, SystemSnapshot, Task, TaskEvent, WorkspaceChanges, WorkspaceDelivery, WorkspaceIsolation, WorkspaceListing} from './types';
import './App.css';

const isMacOS = typeof navigator !== 'undefined' && /Mac/.test(navigator.platform);

const statusLabel: Record<string, string> = {
  draft: 'Draft',
  queued: 'Queued', starting: 'Starting', idle: 'Ready', running: 'Working', waiting: 'Approval needed',
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
  const [modelConformance, setModelConformance] = useState<ModelConformance | null>(null);
  const [verifyingModel, setVerifyingModel] = useState(false);
  const [attachments, setAttachments] = useState<ImageContent[]>([]);
  const [editingNeedsImages, setEditingNeedsImages] = useState(false);
  const [optimisticPrompt, setOptimisticPrompt] = useState<{taskId: string; text: string; time: string; attachments: ImageContent[]} | null>(null);
  const [showDeployment, setShowDeployment] = useState(false);
  const [deploymentStatus, setDeploymentStatus] = useState<DeploymentStatus | null>(null);
  const [activityClock, setActivityClock] = useState(Date.now());
  const [busy, setBusy] = useState(false);
  const [eventsLoading, setEventsLoading] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [showBoard, setShowBoard] = useState(false);
  const [showWorkspace, setShowWorkspace] = useState(false);
  const [showAbout, setShowAbout] = useState(false);
  const [showExtensions, setShowExtensions] = useState(false);
  const [showChanges, setShowChanges] = useState(false);
	const [workspaceInspection, setWorkspaceInspection] = useState<{taskId: string; loading: boolean; result?: WorkspaceIsolation} | null>(null);
  const [appVersion, setAppVersion] = useState('');
  const [unreadTasks, setUnreadTasks] = useState<Set<string>>(new Set());
  const [showInspector, setShowInspector] = useState(false);
  const [collapsedProjects, setCollapsedProjects] = useState<Set<string>>(new Set());
  const [openMenu, setOpenMenu] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<{kind: 'conversation' | 'project'; label: string; taskIds: string[]; retainsWorktree?: boolean} | null>(null);
  const [renamingTask, setRenamingTask] = useState(false);
  const [renameValue, setRenameValue] = useState('');
  const [watchRevision, setWatchRevision] = useState(0);
  const [watchStatus, setWatchStatus] = useState<TaskWatchStatus | null>(null);
  const [hasNewOutput, setHasNewOutput] = useState(false);
  const startupStarted = useRef(false);
  const timelineRef = useRef<HTMLDivElement>(null);
  const composerRef = useRef<HTMLTextAreaElement>(null);
  const followsOutput = useRef(true);
  const taskDrafts = useRef(new Map<string, {text: string; editingMessage: number | null; attachments: ImageContent[]; editingNeedsImages: boolean}>());
  const previousTaskId = useRef('');
  const activeBoardId = useRef('');
  const selectedTaskId = useRef('');
  const snapshotSampling = useRef(false);
  const modelHealthRequest = useRef(0);
  const modelVerificationRequest = useRef(0);
  const connectionRequest = useRef(0);
  const connectionTarget = useRef('');
  const watchRetryAttempt = useRef(0);
  const watchRetryTimer = useRef<number | null>(null);
  const taskStatusHistory = useRef(new Map<string, string>());

  const boardId = connection?.board.id ?? '';

  const refreshTasks = useCallback(async (targetBoard = boardId) => {
    if (!targetBoard) return;
    const expectedTask = selectedTaskId.current;
    try {
      const page = await api.tasks(targetBoard);
      if (targetBoard !== activeBoardId.current) return;
      let latestAttention = '';
      const attentionTaskIds: string[] = [];
      for (const task of page.tasks ?? []) {
        const attention = taskAttention(taskStatusHistory.current.get(task.id) ?? '', task.status, task.id === selectedTaskId.current);
        if (attention) {
          attentionTaskIds.push(task.id);
          latestAttention ||= `${attention}: ${task.name}`;
        }
        taskStatusHistory.current.set(task.id, task.status);
      }
      setUnreadTasks((current) => {
        const next = new Set(current);
        for (const taskId of attentionTaskIds) next.add(taskId);
        return next;
      });
      if (latestAttention) setNotice(latestAttention);
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
    modelVerificationRequest.current += 1;
    setCheckingModel(false);
    setVerifyingModel(false);
    setModelHealth(null);
    setModelConformance(null);
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
      taskStatusHistory.current = new Map((page.tasks ?? []).map((task) => [task.id, task.status]));
      setUnreadTasks(new Set());
      selectedTaskId.current = initialTask?.id ?? '';
      setSelectedTask(initialDetail ?? initialTask);
      setSnapshot(nextSnapshot);
      setEvents([]);
      setOptimisticPrompt(null);
      setError('');
      setShowBoard(false);
      setShowChanges(false);
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
    Promise.all([api.listBoards(), api.appVersion()]).then(([saved, version]) => {
      setAppVersion(version);
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
      if (['task.queued', 'task.starting', 'task.running', 'task.idle', 'task.cancelled', 'task.failed', 'task.interrupted', 'task.stopped', 'approval.requested'].includes(event.normalized?.type ?? '')) void refreshTasks();
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
      if (composer || attachments.length) taskDrafts.current.set(previous, {text: composer, editingMessage, attachments, editingNeedsImages});
      else taskDrafts.current.delete(previous);
    }
    if (previous !== nextTaskId) {
      const draft = taskDrafts.current.get(nextTaskId);
      setComposer(draft?.text ?? '');
      setEditingMessage(draft?.editingMessage ?? null);
      setAttachments(draft?.attachments ?? []);
      setEditingNeedsImages(draft?.editingNeedsImages ?? false);
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
    if (!modelConformance) return;
    const remaining = Date.parse(modelConformance.expiresAt) - Date.now();
    if (remaining <= 0) {
      setModelConformance(null);
      return;
    }
    const timer = window.setTimeout(() => setModelConformance((current) => current?.expiresAt === modelConformance.expiresAt ? null : current), remaining);
    return () => window.clearTimeout(timer);
  }, [modelConformance]);

  useEffect(() => {
    const textarea = composerRef.current;
    if (!textarea) return;
    textarea.style.height = 'auto';
    textarea.style.height = `${Math.min(180, Math.max(44, textarea.scrollHeight))}px`;
  }, [composer]);

  const visibleTasks = useMemo(() => {
    const query = search.trim().toLowerCase();
    const available = selectedTask?.id.startsWith('draft:') ? [selectedTask, ...tasks] : tasks;
	return query ? available.filter((task) => `${task.name} ${task.projectCwd || task.cwd} ${task.status}`.toLowerCase().includes(query)) : available;
  }, [search, selectedTask, tasks]);
  const conversation = useMemo(() => buildConversation(events), [events]);
  const projects = useMemo(() => groupTasksByProject(visibleTasks), [visibleTasks]);
  const activeApproval = selectedTask?.pendingApprovals?.find((approval) => approval.active);
  const selectedComposerMode = selectedTask ? composerMode(selectedTask) : 'send';
  const draftSelected = Boolean(selectedTask?.id.startsWith('draft:'));
	const workspaceInspectionLoading = Boolean(draftSelected && workspaceInspection?.taskId === selectedTask?.id && workspaceInspection?.loading);
  const awaitingFirstPrompt = Boolean(selectedTask?.awaitingPrompt);
	const composerBlocked = busy || workspaceInspectionLoading || connectionState !== 'online' || (editingNeedsImages && attachments.length === 0) || (selectedTask ? (!draftSelected && composerIsBlocked(selectedTask.status)) : true);
  const activeTaskCount = tasks.filter((task) => !terminalStatuses.has(task.status)).length;
  const workflowStarters = rdkWorkflows(snapshot?.boardId);
  const selectedModel = selectedTask?.model ?? '';
  const selectedTaskRecovery = taskRecovery(selectedTask);
  const modelPickerValue = selectedModel.startsWith('drobotics/') ? selectedModel : '';
  const effectiveModel = resolveEffectiveModel(models, selectedModel);
  const currentModelHealth = resolveCurrentModelHealth(modelHealth, effectiveModel);
  const currentModelConformance = modelConformance
    && effectiveModel
    && modelConformance.provider === effectiveModel.provider
    && modelConformance.model === effectiveModel.id
    && Date.parse(modelConformance.expiresAt) > Date.now()
      ? modelConformance
      : null;
  const imageInputSupported = modelAcceptsImages(models, selectedModel);
  const selectedPermissionMode = selectedTask?.permissionMode ?? 'ask';
  const selectedSandboxMode = selectedTask?.sandboxMode ?? (selectedPermissionMode === 'review' ? 'review' : 'workspace');
  const canCreateBlankSideTask = Boolean(connection?.capabilities?.capabilities.includes('tasks.fork.deferred-prompt.v1'));
  const canChangeModel = Boolean(connectionState === 'online' && selectedTask && (draftSelected || selectedTask.status === 'idle' || terminalStatuses.has(selectedTask.status)) && !busy);
  const canChangePermissions = canChangeModel;
  const canChangeSandbox = Boolean(connection?.capabilities?.capabilities.includes('tasks.sandbox.v1') && selectedTask && (draftSelected || selectedTask.status === 'queued' || terminalStatuses.has(selectedTask.status)) && !busy && connectionState === 'online');
  const canStopTask = Boolean(selectedTask && ['queued', 'starting', 'running', 'waiting', 'stopping'].includes(selectedTask.status));
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
		workspaceMode: connection?.capabilities?.capabilities.includes('workspaces.isolation.v1') ? selectedTask.workspaceMode || 'shared' : undefined,
		sandboxMode: connection?.capabilities?.capabilities.includes('tasks.sandbox.v1') ? selectedSandboxMode : undefined,
	  });
      else if (editingMessage !== null) nextTask = await api.forkTask(boardId, {taskId: selectedTask.id, sequence: editingMessage, prompt, images: submittedImages, kind: 'edit', model: selectedModel});
      else if (selectedComposerMode === 'resume') nextTask = await api.resumeTask(boardId, selectedTask.id, prompt, submittedImages);
      else if (selectedComposerMode === 'restart') nextTask = await api.restartTask(boardId, selectedTask.id, prompt, submittedImages);
      else await api.sendPrompt(boardId, selectedTask.id, prompt, submittedImages);
      if (isCurrentTarget(boardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) {
        setEditingMessage(null);
        setEditingNeedsImages(false);
      }
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
    setEditingNeedsImages(item.attachments.length > 0);
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
    modelVerificationRequest.current += 1;
    setCheckingModel(false);
    setVerifyingModel(false);
    setModelHealth(null);
    setModelConformance(null);
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

  async function verifyModelConformance() {
    if (!boardId || !effectiveModel || verifyingModel || !connection?.capabilities?.capabilities.includes('models.conformance.v1')) return;
    const targetBoard = boardId;
    const request = ++modelVerificationRequest.current;
    setVerifyingModel(true);
    setError('');
    try {
      const result = await api.modelConformance(targetBoard, `${effectiveModel.provider}/${effectiveModel.id}`, Boolean(currentModelConformance));
      if (modelVerificationRequest.current === request && activeBoardId.current === targetBoard) setModelConformance(result);
    } catch (reason) {
      if (modelVerificationRequest.current === request && activeBoardId.current === targetBoard) setError(friendlyError(String(reason)));
    } finally {
      if (modelVerificationRequest.current === request) setVerifyingModel(false);
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

  async function changeSandboxMode(mode: string) {
    if (!selectedTask || !boardId || !canChangeSandbox) return;
    if (draftSelected) {
      setSelectedTask({...selectedTask, sandboxMode: mode as Task['sandboxMode']});
      return;
    }
    const sourceTaskId = selectedTask.id;
    setBusy(true);
    setError('');
    try {
      const task = await api.setSandboxMode(boardId, selectedTask.id, mode);
      if (isCurrentTarget(boardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setSelectedTask(task);
      if (activeBoardId.current === boardId) setTasks((current) => current.map((item) => item.id === task.id ? task : item));
    } catch (reason) {
      if (isCurrentTarget(boardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setError(friendlyError(String(reason)));
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
	  const projectCwd = cwd || selectedTask?.projectCwd || selectedTask?.cwd || '/root';
    setRenamingTask(false);
    setOpenMenu('');
    setEvents([]);
    setOptimisticPrompt(null);
    selectedTaskId.current = id;
	  setSelectedTask({
		id,
		name: 'New task',
		cwd: projectCwd,
		projectCwd,
		workspaceMode: 'shared',
      status: 'draft',
      createdAt: now,
      updatedAt: now,
      lastSequence: 0,
      model: models[0] ? `${models[0].provider}/${models[0].id}` : '',
      permissionMode: 'ask',
	  sandboxMode: 'workspace',
	  });
	  const canInspect = Boolean(boardId && connection?.capabilities?.capabilities.includes('workspaces.isolation.v1'));
	  setWorkspaceInspection(canInspect ? {taskId: id, loading: true} : null);
	  if (canInspect) {
		void api.inspectWorkspaceIsolation(boardId, projectCwd).then((result) => {
		  if (activeBoardId.current !== boardId || selectedTaskId.current !== id) return;
		  setWorkspaceInspection({taskId: id, loading: false, result});
		  setSelectedTask((current) => current?.id === id ? {...current, workspaceMode: result.eligible ? result.recommendedMode : 'shared'} : current);
		}).catch((reason) => {
		  if (activeBoardId.current !== boardId || selectedTaskId.current !== id) return;
		  setWorkspaceInspection({taskId: id, loading: false});
		  setNotice(`Workspace isolation is unavailable. This task will use the shared directory. ${friendlyError(String(reason))}`);
		});
	  }
	  window.requestAnimationFrame(() => composerRef.current?.focus());
	}

	function changeWorkspaceMode(mode: string) {
	  if (!selectedTask || !draftSelected || workspaceInspectionLoading) return;
	  const eligible = workspaceInspection?.taskId === selectedTask.id && workspaceInspection.result?.eligible;
	  if (mode === 'worktree' && !eligible) return;
	  setSelectedTask({...selectedTask, workspaceMode: mode as Task['workspaceMode']});
	}

  function beginRename(task: Task) {
    selectTask(task);
    setRenameValue(task.name);
    setRenamingTask(true);
    setOpenMenu('');
  }

  async function createSideTask() {
    if (!selectedTask || !boardId) return;
    const sourceBoardId = boardId;
    const sourceTaskId = selectedTask.id;
    setBusy(true);
    setError('');
    try {
      const task = await api.forkTask(boardId, {taskId: selectedTask.id, kind: 'side', model: selectedModel});
      await refreshTasks();
      if (!isCurrentTarget(sourceBoardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) return;
      selectedTaskId.current = task.id;
      setSelectedTask(task);
      setEvents([]);
      setOptimisticPrompt(null);
      window.requestAnimationFrame(() => composerRef.current?.focus());
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
	setShowChanges(false);
	setWorkspaceInspection(null);
    setRenamingTask(false);
    if (selectedTask?.id.startsWith('draft:') && selectedTask.id !== task.id) {
      taskDrafts.current.delete(selectedTask.id);
    }
    selectedTaskId.current = task.id;
    setUnreadTasks((current) => {
      const next = new Set(current);
      next.delete(task.id);
      return next;
    });
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
        <button className="version-button" title="Version and updates" onClick={() => setShowAbout(true)}>{appVersion ? `v${appVersion}` : <LoaderCircle size={12} className="spin" />}</button>
        {connection?.capabilities?.capabilities.includes('extensions.catalog.v1') && <button className="icon-button" title="Capabilities" disabled={connectionState !== 'online'} onClick={() => setShowExtensions(true)}><Box size={16} /></button>}
        {connection?.capabilities?.capabilities.includes('support.bundle.v1') && <button className="icon-button" title="Save private support bundle" disabled={busy || connectionState !== 'online'} onClick={() => void saveSupportBundle()}><Download size={16} /></button>}
        <button className="icon-button" title={connectionState === 'offline' ? 'Reconnect board' : 'Sync board now'} disabled={refreshing || !connection} onClick={() => void refreshWorkspace()}><RefreshCw size={16} className={refreshing ? 'spin' : ''} /></button>
        <button className={`icon-button ${showInspector ? 'active' : ''}`} title="Board monitor" onClick={() => setShowInspector((value) => !value)}><PanelRight size={17} /></button>
      </header>

      <aside className="task-sidebar">
        <div className="sidebar-heading">
          <div><span className="section-label">Projects</span><span className="task-count">{projects.length}</span></div>
          <button className="icon-button compact" title="New conversation" onClick={() => setShowWorkspace(true)} disabled={!connection || connectionState !== 'online'}><Plus size={17} /></button>
        </div>
        <label className="search-field"><Search size={15} /><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search projects and tasks" /></label>
        <div className="task-list">
          {projects.map((project) => <section className={`project-group ${collapsedProjects.has(project.path) ? 'collapsed' : ''}`} key={project.path}>
            <div className="project-heading">
              <button className="project-toggle" onClick={() => toggleProject(project.path)} title={collapsedProjects.has(project.path) ? 'Expand project' : 'Collapse project'}><ChevronRight className="project-chevron" size={13} /><Folder size={14} /><span>{project.name}</span><small>{arrangeTasks(project.tasks).length}</small></button>
              <button className="row-more project-add" title={`New conversation in ${project.name}`} onClick={() => openNewTask(project.path)}><Plus size={14} /></button>
              <button className="row-more" title="Project actions" onClick={() => setOpenMenu((current) => current === `project:${project.path}` ? '' : `project:${project.path}`)}><MoreHorizontal size={15} /></button>
			  {openMenu === `project:${project.path}` && <div className="row-menu"><button className="destructive" onClick={() => setDeleteTarget({kind: 'project', label: project.name, taskIds: project.tasks.map((task) => task.id), retainsWorktree: project.tasks.some((task) => task.workspaceMode === 'worktree')})}><Trash2 size={14} />Remove project</button></div>}
            </div>
            {!collapsedProjects.has(project.path) && <div className="project-conversations">{arrangeTasks(project.tasks).map(({task, depth, branchKind}) => <div key={task.id} className={`task-row-shell ${depth ? 'branch-task' : ''}`} style={{'--task-depth': depth} as any}>
              <button className={`task-row ${selectedTask?.id === task.id ? 'selected' : ''}`} onClick={() => {selectTask(task); setOpenMenu('');}}>
                {depth ? <CornerDownRight className="branch-mark" size={13} /> : <span className={`task-state-dot dot-${task.awaitingPrompt ? 'idle' : task.status}`} />}
                <span className="task-row-main"><span className="task-row-name">{task.name}</span>{depth > 0 && <span className="task-row-path">{branchKind === 'side' ? 'Side Agent' : 'Branch'}</span>}</span>
                <span className={`task-row-time ${unreadTasks.has(task.id) ? 'unread' : ''}`}>{unreadTasks.has(task.id) && <i />}{relativeTime(task.updatedAt)}</span>
              </button>
              <button className="row-more task-more" title="Conversation actions" onClick={() => setOpenMenu((current) => current === `task:${task.id}` ? '' : `task:${task.id}`)}><MoreHorizontal size={15} /></button>
			  {openMenu === `task:${task.id}` && <div className="row-menu task-menu"><button onClick={() => beginRename(task)}><FilePenLine size={14} />Rename conversation</button>{!task.id.startsWith('draft:') && <button className="destructive" onClick={() => setDeleteTarget({kind: 'conversation', label: task.name, taskIds: [task.id], retainsWorktree: task.workspaceMode === 'worktree'})}><Trash2 size={14} />Delete conversation</button>}</div>}
            </div>)}</div>}
          </section>)}
          {visibleTasks.length === 0 && <div className="empty-state"><ListTodo size={21} /><span>No tasks</span></div>}
        </div>
        {connection && <button className="board-summary" onClick={() => setShowBoard(true)}>
          <Server size={16} /><span><strong>{connection.board.name}</strong><small>{connection.daemon?.activeTasks ?? activeTaskCount}/{connection.daemon?.maximumTasks ?? 0} active{connection.daemon?.queuedTasks ? ` · ${connection.daemon.queuedTasks} queued` : ''}</small></span><ChevronRight size={15} />
        </button>}
      </aside>

      <main className="task-main">
        {selectedTask ? <>
          <div className="task-header">
			<div className="task-title-block"><div className="task-title-line">{renamingTask ? <form className="title-editor" onSubmit={(event) => {event.preventDefault(); void renameSelectedTask();}}><input value={renameValue} maxLength={64} autoFocus onChange={(event) => setRenameValue(event.target.value)} onBlur={() => void renameSelectedTask()} onKeyDown={(event) => {if (event.key === 'Escape') {setRenameValue(selectedTask.name); setRenamingTask(false);}}} /></form> : <><h1 title="Double-click to rename" onDoubleClick={() => beginRename(selectedTask)}>{selectedTask.name}</h1><button className="title-edit" title="Rename conversation" onClick={() => beginRename(selectedTask)}><FilePenLine size={13} /></button></>}<span className={`status status-${awaitingFirstPrompt ? 'idle' : selectedTask.status}`}>{awaitingFirstPrompt ? 'Ready' : statusLabel[selectedTask.status] ?? selectedTask.status}</span>{selectedTask.workspaceMode === 'worktree' && <span className="workspace-mode-badge">Isolated</span>}{watchStatus && <span className={`stream-status stream-${watchStatus.state}`} role="status" title={watchStatus.message}><RefreshCw size={11} className="spin" />{watchStatusLabel(watchStatus)}</span>}</div><span className="workspace-path">{selectedTask.projectCwd || selectedTask.cwd}</span></div>
            <div className="task-actions">
              {!draftSelected && connection?.capabilities?.capabilities.includes('workspaces.changes.v1') && <button className="secondary-button changes-button" title="Review current workspace changes" onClick={() => setShowChanges(true)} disabled={connectionState !== 'online'}><FileDiff size={15} />Changes</button>}
              {!draftSelected && <button className="secondary-button side-task-button" title={!selectedTask.sessionFile ? 'Side Agent is available after the first response' : !canCreateBlankSideTask ? 'Update Hobot Code on the board to open blank Side Agent conversations' : 'Open an independent conversation from this context'} onClick={() => void createSideTask()} disabled={busy || !selectedTask.sessionFile || !canCreateBlankSideTask}><GitBranch size={15} />Side Agent</button>}
              {terminalStatuses.has(selectedTask.status) && !awaitingFirstPrompt && <button className="secondary-button" onClick={() => composerRef.current?.focus()}><RefreshCw size={14} />{selectedComposerMode === 'resume' ? 'Resume' : 'New session'}</button>}
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
              {selectedTask.status === 'queued' ? <div className="agent-progress immediate"><ListTodo size={14} /><span>Waiting for a board Agent slot</span><small>{selectedTask.queuedAt ? relativeTime(selectedTask.queuedAt) : 'queued'}</small></div> : ['starting', 'running'].includes(selectedTask.status) && <AgentProgress startedAt={activityStart} now={activityClock} hasOutput={!optimisticPrompt && latestConversationItem?.kind === 'assistant' && Boolean(latestConversationItem.text || latestConversationItem.thinking || latestConversationItem.tools.length)} />}
              {selectedTaskRecovery && <TaskRecoveryCard presentation={selectedTaskRecovery} canCheckModel={Boolean(effectiveModel && connection?.capabilities?.capabilities.includes('models.health.v1'))} canDiagnose={Boolean(connection?.capabilities?.capabilities.includes('support.bundle.v1'))} busy={busy || checkingModel} onAction={() => {if (selectedTaskRecovery.recovery === 'check-model') {void checkModelHealth(); return;} if (selectedTaskRecovery.recovery === 'diagnose') {void saveSupportBundle(); return;} setComposer(selectedTaskRecovery.action?.prompt ?? ''); window.requestAnimationFrame(() => composerRef.current?.focus());}} />}
            </div>
          </div>

          {hasNewOutput && <button className="jump-latest" onClick={scrollToLatest}><ArrowDown size={15} />New output</button>}
          <div className="composer-dock">
            {activeApproval && <ApprovalBar key={activeApproval.id} approval={activeApproval} busy={busy} respond={(response) => respond(activeApproval, response)} />}
            <form className="composer" onSubmit={submitPrompt}>
              {editingMessage !== null && <div className="editing-banner"><FilePenLine size={14} /><span>{editingNeedsImages ? attachments.length ? 'Current attachments will replace the original images.' : 'Reattach the original images, or continue without them.' : 'Editing this message. Later messages will be replaced.'}</span>{editingNeedsImages && attachments.length === 0 && <button type="button" className="text-button" onClick={() => setEditingNeedsImages(false)}>Continue without images</button>}<button type="button" title="Cancel edit" onClick={() => {setEditingMessage(null); setEditingNeedsImages(false); setComposer('');}}><X size={14} /></button></div>}
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
                placeholder={awaitingFirstPrompt ? 'Message this Side Agent' : selectedComposerMode === 'resume' ? 'Continue this task' : selectedComposerMode === 'restart' ? 'Start a new session' : 'Message Hobot Code'}
                rows={1}
              />
			  <div className="composer-footer">
				<ImagePickerButton disabled={busy || (composerBlocked && !editingNeedsImages) || connectionState !== 'online' || attachments.length >= 4 || !imageInputSupported} title={imageInputSupported ? 'Attach images' : `${effectiveModel?.name || 'The selected model'} does not support image input`} onPick={(images) => {try {setAttachments(appendImages(attachments, images));} catch (reason) {setError(friendlyError(String(reason)));}}} onError={setError} />
				{draftSelected && connection?.capabilities?.capabilities.includes('workspaces.isolation.v1') && <label className="workspace-mode-picker" title={workspaceInspectionLoading ? 'Checking workspace' : workspaceInspection?.result?.reason || 'Choose whether this task shares the project directory'}><GitBranch size={13} /><select aria-label="Workspace mode" value={selectedTask.workspaceMode || 'shared'} disabled={workspaceInspectionLoading} onChange={(event) => changeWorkspaceMode(event.target.value)}><option value="shared">Shared</option><option value="worktree" disabled={!workspaceInspection?.result?.eligible}>Isolated</option></select><ChevronDown size={12} /></label>}
                <label className="model-picker" title={canChangeModel ? 'Choose model' : 'Stop the current turn before changing models'}><select aria-label="Model" value={modelPickerValue} disabled={!canChangeModel} onChange={(event) => void changeModel(event.target.value)}><option value="" disabled>Board default</option>{selectedModel.startsWith('drobotics/') && !models.some((model) => `${model.provider}/${model.id}` === selectedModel) && <option value={selectedModel}>{selectedModel.split('/').at(-1)}</option>}{models.map((model) => <option key={`${model.provider}/${model.id}`} value={`${model.provider}/${model.id}`}>{model.name || model.id}</option>)}</select><ChevronDown size={12} /></label>
                {connection?.capabilities?.capabilities.includes('models.health.v1') && <button className={`model-health-button ${currentModelHealth?.status ?? ''}`} type="button" title={currentModelHealth ? `${currentModelHealth.message} Click to check again.${currentModelHealth.cached ? ' Cached result.' : ''}` : 'Check model availability'} onClick={() => void checkModelHealth()} disabled={checkingModel || verifyingModel || !effectiveModel}>{checkingModel ? <LoaderCircle size={12} className="spin" /> : currentModelHealth?.status === 'available' ? <Check size={12} /> : currentModelHealth?.status === 'unavailable' ? <XCircle size={12} /> : <Activity size={12} />}<span>{checkingModel ? 'Checking' : currentModelHealth?.status === 'available' ? `${currentModelHealth.latencyMs ?? 0} ms` : currentModelHealth?.status === 'unavailable' ? modelHealthLabel(currentModelHealth.category) : 'Check'}</span></button>}
                {connection?.capabilities?.capabilities.includes('models.conformance.v1') && <button className={`model-health-button model-verify-button ${currentModelConformance?.status ?? ''}`} type="button" title={currentModelConformance ? `${currentModelConformance.message} ${currentModelConformance.checks.map((check) => `${check.name}: ${check.status}`).join(', ')}. Click to verify again.` : 'Verify streaming, tools, continuation, and declared input modes'} onClick={() => void verifyModelConformance()} disabled={checkingModel || verifyingModel || !effectiveModel}>{verifyingModel ? <LoaderCircle size={12} className="spin" /> : currentModelConformance?.status === 'verified' ? <ShieldCheck size={12} /> : currentModelConformance?.status === 'failed' ? <AlertTriangle size={12} /> : <ShieldCheck size={12} />}<span>{verifyingModel ? 'Verifying' : currentModelConformance?.status === 'verified' ? 'Verified' : currentModelConformance?.status === 'compatible' ? 'Compatible' : currentModelConformance?.status === 'failed' ? 'Limited' : 'Verify'}</span></button>}
                <label className="permission-picker" title={canChangePermissions ? 'Choose approval mode' : 'Stop the current turn before changing permissions'}><ShieldCheck size={13} /><select aria-label="Approval mode" value={selectedPermissionMode} disabled={!canChangePermissions} onChange={(event) => void changePermissionMode(event.target.value)}><option value="review">Review only</option><option value="ask">Ask for changes</option><option value="developer">Developer</option></select><ChevronDown size={12} /></label>
				{connection?.capabilities?.capabilities.includes('tasks.sandbox.v1') && <label className="permission-picker" title={canChangeSandbox ? 'Choose the board-side OS boundary. Network remains available for the model gateway.' : connection.capabilities.sandbox?.reason || 'Stop the current turn before changing the OS boundary'}><ShieldCheck size={13} /><select aria-label="OS sandbox" value={selectedSandboxMode} disabled={!canChangeSandbox} onChange={(event) => void changeSandboxMode(event.target.value)}><option value="review" disabled={!connection.capabilities.sandbox?.available}>Read only</option><option value="workspace" disabled={!connection.capabilities.sandbox?.available}>Workspace</option><option value="system" disabled={!connection.capabilities.sandbox?.available}>Board hardware</option><option value="off">No sandbox</option></select><ChevronDown size={12} /></label>}
				<span className="composer-state">{connectionState !== 'online' ? 'Offline · draft preserved' : workspaceInspectionLoading ? 'Checking workspace' : draftSelected || awaitingFirstPrompt ? 'Starts when sent' : editingMessage !== null ? 'Replaces later messages' : selectedComposerMode === 'resume' ? 'Resume session' : selectedComposerMode === 'restart' ? 'New session' : statusLabel[selectedTask.status] ?? selectedTask.status}</span>
                {connectionState !== 'online' ? <button className="send-button reconnect-mode" type="button" title="Reconnect" onClick={() => void refreshWorkspace()} disabled={refreshing}><RefreshCw size={15} className={refreshing ? 'spin' : ''} /></button> : canStopTask ? <button className="send-button stop-mode" type="button" title="Stop" onClick={stopTask} disabled={busy || selectedTask.status === 'stopping'}><Square size={14} fill="currentColor" /></button> : <button className="send-button" type="submit" title="Send" disabled={!composer.trim() || composerBlocked}><ArrowUp size={17} /></button>}
              </div>
            </form>
          </div>
        </> : <div className="main-empty"><div className="empty-symbol"><Bot size={24} /></div><strong>Select a task</strong><span>Choose an existing conversation or create a new one.</span></div>}
      </main>

      {showInspector && <aside className="inspector">
        <div className="inspector-header"><span>Board monitor</span><div className="inspector-header-actions"><small>{snapshot ? `Updated ${relativeTime(snapshot.capturedAt)}` : 'Not sampled'}</small><button className="icon-button compact" title="Close monitor" onClick={() => setShowInspector(false)}><X size={16} /></button></div></div>
        {connection && <BoardMonitor connection={connection} connectionState={connectionState} snapshot={snapshot} task={selectedTask} />}
        {selectedTask?.sandbox && <TaskSandboxInspector task={selectedTask} />}
        {selectedTask?.deployment && <DeploymentInspector status={deploymentStatus} record={selectedTask.deployment} />}
      </aside>}

      {error && <div className="error-toast"><XCircle size={17} /><span>{friendlyError(error)}</span><button title="Dismiss" onClick={() => setError('')}><X size={15} /></button></div>}
      {notice && <div className="success-toast"><Check size={17} /><span>{notice}</span><button title="Dismiss" onClick={() => setNotice('')}><X size={15} /></button></div>}
      {showBoard && <BoardDialog boards={boards} busy={busy} onClose={() => boards.length > 0 && setShowBoard(false)} onConnect={connect} onSave={async (board) => {const saved = await api.saveBoard(board); setBoards(await api.listBoards()); await connect(saved);}} onRemove={async (board) => {await api.removeBoard(board.id); if (activeBoardId.current === board.id) {activeBoardId.current = ''; setConnection(null); setConnectionState('offline'); setTasks([]); setSelectedTask(null);} setBoards(await api.listBoards());}} />}
	  {showWorkspace && boardId && <WorkspaceDialog boardId={boardId} initialPath={selectedTask?.projectCwd ?? selectedTask?.cwd ?? ''} onClose={() => setShowWorkspace(false)} onChoose={(path) => {setShowWorkspace(false); openNewTask(path);}} />}
      {showAbout && <AboutDialog appVersion={appVersion} connection={connection} onInstall={async () => {if (!connection) throw new Error('Connect a board before updating.'); const result = await api.installBoardUpdate(connection.board.id); setConnection(result.connection); setConnectionState('online'); setSnapshot(result.connection.snapshot ?? null); setWatchRevision((revision) => revision + 1); void refreshWorkspace(); return result;}} onClose={() => setShowAbout(false)} />}
      {showExtensions && connection && <ExtensionCenterDialog boardId={connection.board.id} boardName={connection.board.name} boardTarget={snapshot?.boardId || connection.compatibility?.boardId || ''} onClose={() => setShowExtensions(false)} />}
      {showDeployment && selectedTask && snapshot && <DeploymentDialog boardId={boardId} cwd={selectedTask.cwd} snapshot={snapshot} models={models} busy={busy} onClose={() => setShowDeployment(false)} onStart={startDeployment} />}
	      {showChanges && selectedTask && boardId && <WorkspaceChangesDialog boardId={boardId} task={selectedTask} canDeliver={Boolean(connection?.capabilities?.capabilities.includes('workspaces.delivery.v1'))} onClose={() => setShowChanges(false)} />}
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

function TaskRecoveryCard({presentation, canCheckModel, canDiagnose, busy, onAction}: {presentation: NonNullable<ReturnType<typeof taskRecovery>>; canCheckModel: boolean; canDiagnose: boolean; busy: boolean; onAction: () => void}) {
  const available = taskRecoveryActionAvailable(presentation.recovery, canCheckModel, canDiagnose);
  return <section className="task-recovery" role="alert"><div className="turn-failure-heading"><AlertTriangle size={16} /><strong>{presentation.title}</strong></div><p>{presentation.message}</p>{presentation.action && <div className="turn-failure-actions"><button className="secondary-button" type="button" disabled={busy || !available} onClick={onAction}>{presentation.recovery === 'diagnose' ? <Download size={14} /> : presentation.recovery === 'check-model' ? <Activity size={14} /> : <RefreshCw size={14} />}{presentation.action.label}</button></div>}</section>;
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

function BoardDialog({boards, busy, onClose, onConnect, onSave, onRemove}: {boards: Board[]; busy: boolean; onClose: () => void; onConnect: (board: Board) => void; onSave: (board: Board) => Promise<void>; onRemove: (board: Board) => Promise<void>}) {
  const [editing, setEditing] = useState(boards.length === 0);
  const [form, setForm] = useState<Board>({id: '', name: 'RDK S100', host: '', user: 'root', port: 22, identityFile: ''});
  const [working, setWorking] = useState(false);
  const [probe, setProbe] = useState<Connection | null>(null);
  const [failure, setFailure] = useState('');
  const [removing, setRemoving] = useState<Board | null>(null);
  const beginEdit = (board?: Board) => {setForm(board ?? {id: '', name: 'RDK S100', host: '', user: 'root', port: 22, identityFile: ''}); setProbe(null); setFailure(''); setEditing(true);};
  const submit = async () => {
    setWorking(true); setFailure(''); setProbe(null);
    try {
      const result = await api.probeBoard(form);
      setProbe(result);
      if (!result.connected) {setFailure(result.error || result.compatibility?.summary || 'Could not connect to this board.'); return;}
      const detected = result.snapshot?.boardId?.toUpperCase();
      const candidate = detected && /^(RDK (S100|S600|X5))$/i.test(form.name) ? {...form, name: `RDK ${detected}`} : form;
      await onSave(candidate);
    } catch (reason) { setFailure(friendlyError(String(reason))); }
    finally { setWorking(false); }
  };
  const remove = async () => {if (!removing) return; setWorking(true); setFailure(''); try {await onRemove(removing); setRemoving(null); if (boards.length <= 1) beginEdit();} catch (reason) {setFailure(friendlyError(String(reason)));} finally {setWorking(false);}};
  const disabled = busy || working;
  return <div className="modal-backdrop"><div className="modal board-modal"><div className="modal-header"><div><span className="modal-eyebrow">Boards</span><h2>{editing ? (form.id ? 'Edit board' : 'Add board') : 'Connect'}</h2></div>{boards.length > 0 && <button className="icon-button" title="Close" onClick={onClose}><X size={18} /></button>}</div>{!editing ? <><div className="saved-boards">{boards.map((board) => <div className="saved-board-row" key={board.id}><button className="saved-board" onClick={() => onConnect(board)} disabled={disabled}><Server size={19} /><span><strong>{board.name}</strong><small>{board.user}@{board.host}:{board.port}</small></span><ChevronRight size={15} /></button><button className="icon-button compact" title={`Edit ${board.name}`} onClick={() => beginEdit(board)}><FilePenLine size={14} /></button><button className="icon-button compact danger-icon" title={`Remove ${board.name}`} onClick={() => setRemoving(board)}><Trash2 size={14} /></button></div>)}</div><button className="add-board-row" onClick={() => beginEdit()}><Plus size={16} />Add board</button></> : <form onSubmit={(event) => {event.preventDefault(); void submit();}} className="form-grid"><div className="board-presets">{boardPresets.map((preset) => <button type="button" key={preset.name} className={form.name === preset.name ? 'selected' : ''} onClick={() => setForm({...form, ...preset})}><Server size={14} /><span>{preset.name}</span></button>)}</div><label><span>Name</span><input value={form.name} onChange={(event) => setForm({...form, name: event.target.value})} required /></label><label><span>Host</span><input value={form.host} onChange={(event) => setForm({...form, host: event.target.value})} placeholder="Board IP or hostname" autoFocus required /></label><div className="form-row"><label><span>User</span><input value={form.user} onChange={(event) => setForm({...form, user: event.target.value})} required /></label><label><span>Port</span><input type="number" min="1" max="65535" value={form.port} onChange={(event) => setForm({...form, port: Number(event.target.value)})} required /></label></div><label><span>Identity file</span><input value={form.identityFile} onChange={(event) => setForm({...form, identityFile: event.target.value})} placeholder="Use SSH agent or config" /></label>{failure && <ConnectionFailure result={probe} message={failure} />} {probe?.connected && probe.snapshot && <div className="probe-success"><Check size={14} /><span>Detected {probe.snapshot.board} · RDK OS {probe.snapshot.rdkOsVersion}</span></div>}<div className="modal-actions">{boards.length > 0 && <button type="button" className="secondary-button" onClick={() => setEditing(false)}>Back</button>}<button className="primary-button" type="submit" disabled={disabled}>{disabled ? <LoaderCircle size={15} className="spin" /> : <Server size={15} />}Verify, save & connect</button></div></form>}{removing && <div className="inline-confirm"><AlertTriangle size={16} /><span><strong>Remove {removing.name}?</strong><small>Board tasks keep running; only this saved connection is removed.</small></span><button className="secondary-button" onClick={() => setRemoving(null)} disabled={disabled}>Cancel</button><button className="danger-button" onClick={() => void remove()} disabled={disabled}>Remove</button></div>}</div></div>;
}

function ConnectionFailure({result, message}: {result: Connection | null; message: string}) {
  return <div className="connection-failure" role="alert"><AlertTriangle size={15} /><span><strong>{message}</strong>{result?.compatibility?.issues.map((issue) => <small key={issue.code}>{issue.action || issue.message}</small>)}<small>Check the VPN, SSH access, and that `hobot daemon start` is running.</small></span></div>;
}

function WorkspaceDialog({boardId, initialPath, onClose, onChoose}: {boardId: string; initialPath: string; onClose: () => void; onChoose: (path: string) => void}) {
  const [listing, setListing] = useState<WorkspaceListing | null>(null);
  const [loading, setLoading] = useState(true);
  const [failure, setFailure] = useState('');
  const [folderName, setFolderName] = useState('');
  const load = useCallback((path: string) => {setLoading(true); setFailure(''); api.browseWorkspace(boardId, path).then(setListing).catch((reason) => setFailure(friendlyError(String(reason)))).finally(() => setLoading(false));}, [boardId]);
  useEffect(() => {load(initialPath);}, [initialPath, load]);
  const create = async () => {if (!listing || !folderName.trim()) return; setLoading(true); setFailure(''); try {const next = await api.createWorkspace(boardId, listing.path, folderName.trim()); setFolderName(''); setListing(next);} catch (reason) {setFailure(friendlyError(String(reason)));} finally {setLoading(false);}};
  return <div className="modal-backdrop"><div className="modal workspace-modal"><div className="modal-header"><div><span className="modal-eyebrow">Project workspace</span><h2>Choose a folder</h2></div><button className="icon-button" title="Close" onClick={onClose}><X size={18} /></button></div><div className="workspace-browser"><div className="workspace-path-bar"><button className="icon-button compact" title="Parent folder" onClick={() => listing?.parent && load(listing.parent)} disabled={loading || !listing?.parent}><ChevronDown className="up-icon" size={15} /></button><span>{listing?.path || initialPath || '/root'}</span></div>{loading && !listing ? <div className="deployment-loading"><LoaderCircle size={16} className="spin" />Loading folders</div> : <div className="workspace-folders">{listing?.directories.map((directory) => <button key={directory.path} onClick={() => load(directory.path)}><Folder size={16} /><span>{directory.name}</span><ChevronRight size={14} /></button>)}{listing && listing.directories.length === 0 && <div className="snapshot-empty">No subfolders</div>}</div>}{failure && <div className="connection-failure"><AlertTriangle size={14} /><span><strong>{failure}</strong></span></div>}<div className="workspace-create"><input value={folderName} onChange={(event) => setFolderName(event.target.value)} placeholder="New folder name" onKeyDown={(event) => {if (event.key === 'Enter') {event.preventDefault(); void create();}}} /><button className="secondary-button" onClick={() => void create()} disabled={loading || !folderName.trim()}><Plus size={14} />Create</button></div><div className="modal-actions"><button className="secondary-button" onClick={onClose}>Cancel</button><button className="primary-button" disabled={!listing || loading} onClick={() => listing && onChoose(listing.path)}><Folder size={15} />Use this folder</button></div></div></div></div>;
}

function WorkspaceChangesDialog({boardId, task, canDeliver, onClose}: {boardId: string; task: Task; canDeliver: boolean; onClose: () => void}) {
  const [changes, setChanges] = useState<WorkspaceChanges | null>(null);
	const [delivery, setDelivery] = useState<WorkspaceDelivery | null>(null);
  const [loading, setLoading] = useState(true);
	const [applying, setApplying] = useState(false);
	const [confirmApply, setConfirmApply] = useState(false);
  const [failure, setFailure] = useState('');
  const request = useRef(0);
	  const deliveryAvailable = canDeliver && task.workspaceMode === 'worktree';
	  const load = useCallback(() => {
		const active = ++request.current;
		setLoading(true);
		setFailure('');
		setConfirmApply(false);
		const deliveryRequest = deliveryAvailable
		  ? api.inspectWorkspaceDelivery(boardId, task.id).catch((reason): WorkspaceDelivery => ({taskId: task.id, ready: false, reason: friendlyError(String(reason))}))
		  : Promise.resolve(null);
		Promise.all([api.workspaceChanges(boardId, task.id), deliveryRequest]).then(([nextChanges, nextDelivery]) => {
		  if (request.current !== active) return;
		  setChanges(nextChanges);
		  setDelivery(nextDelivery);
		}).catch((reason) => {
		  if (request.current === active) setFailure(friendlyError(String(reason)));
		}).finally(() => {
		  if (request.current === active) setLoading(false);
		});
	  }, [boardId, deliveryAvailable, task.id]);
  useEffect(() => {load(); return () => {request.current += 1;};}, [load]);
  const summary = changes ? workspaceChangeSummary(changes) : null;
  const diff = workspaceDiffLines(changes?.patch ?? '');
	  const deliverySummary = workspaceDeliverySummary(delivery);
	  const apply = async () => {
		if (!delivery?.ready) return;
		setApplying(true);
		setFailure('');
		try {
		  const result = await api.applyWorkspace(boardId, task.id, delivery.digest || '');
		  setDelivery({taskId: task.id, ready: false, alreadyApplied: result.applied, digest: result.digest, reason: 'These isolated changes have already been applied to the project.'});
		  setConfirmApply(false);
		} catch (reason) {
		  setFailure(friendlyError(String(reason)));
		} finally {
		  setApplying(false);
		}
	  };
	  return <div className="modal-backdrop"><div className="modal changes-modal"><div className="modal-header"><div><span className="modal-eyebrow">{task.workspaceMode === 'worktree' ? 'Isolated workspace' : 'Current workspace'}</span><h2>Review changes</h2></div><div className="modal-header-actions"><button className="icon-button" title="Refresh changes" onClick={load} disabled={loading || applying}><RefreshCw size={16} className={loading ? 'spin' : ''} /></button><button className="icon-button" title="Close" onClick={onClose} disabled={applying}><X size={18} /></button></div></div>{loading && !changes ? <div className="deployment-loading"><LoaderCircle size={17} className="spin" />Inspecting the board workspace</div> : changes && summary ? <div className="changes-content"><div className="changes-summary"><FileDiff size={17} /><span><strong>{summary.title}</strong><small>{summary.detail}</small></span></div>{deliverySummary && <div className={`delivery-summary delivery-${deliverySummary.tone}`}>{deliverySummary.tone === 'blocked' ? <AlertTriangle size={16} /> : <GitBranch size={16} />}<span><strong>{deliverySummary.title}</strong><small>{deliverySummary.detail}</small></span></div>}{confirmApply && delivery?.ready && <div className="delivery-confirm" role="alert"><ShieldCheck size={16} /><span><strong>Apply to {task.projectCwd || 'the original project'}?</strong><small>Idle Agents using the isolated workspace or original project will stop. The exact reviewed snapshot will be staged for your final Git review.</small></span></div>}{changes.repository && <div className="changes-meta"><span>{changes.scope === '.' ? 'Repository root' : `Scope · ${changes.scope}`}</span>{changes.head && <code>{changes.head}</code>}</div>}{changes.files.length > 0 && <div className="changes-files">{changes.files.map((file) => <div key={`${file.status}:${file.path}`} className={file.conflict ? 'conflict' : ''}><span className="change-kind">{file.status}</span><span className="change-path"><strong>{file.path}</strong>{file.originalPath && <small>from {file.originalPath}</small>}</span><small>{workspaceChangeLabel(file)}</small></div>)}</div>}{changes.filesTruncated && <div className="changes-warning"><AlertTriangle size={13} />More changed files exist on the board.</div>}{diff.lines.length > 0 && <div className="diff-view" role="region" aria-label="Workspace diff">{diff.lines.map((line) => <span key={line.key} className={`diff-${line.kind}`}>{line.text || ' '}</span>)}</div>}{changes.repository && changes.files.length > 0 && !changes.patch && <div className="changes-note">Untracked and binary files are listed without transferring their contents.</div>}{(changes.patchTruncated || diff.truncated) && <div className="changes-warning"><AlertTriangle size={13} />{changes.patchTruncated ? 'The board limited this patch to 512 KiB.' : 'Studio is showing the first 4000 diff lines.'}</div>}</div> : null}{failure && <div className="changes-action-error"><AlertTriangle size={14} />{failure}</div>}<div className="changes-footer"><span>{task.workspaceMode === 'worktree' ? task.projectCwd || task.cwd : task.cwd}</span><div>{confirmApply && <button className="secondary-button" onClick={() => setConfirmApply(false)} disabled={applying}>Cancel</button>}{delivery?.ready && <button className="primary-button" onClick={() => confirmApply ? void apply() : setConfirmApply(true)} disabled={loading || applying}>{applying ? <LoaderCircle size={15} className="spin" /> : <GitBranch size={15} />}{confirmApply ? 'Apply staged changes' : 'Apply to project'}</button>}<button className={delivery?.ready ? 'secondary-button' : 'primary-button'} onClick={onClose} disabled={applying}>Done</button></div></div></div></div>;
}

function AboutDialog({appVersion, connection, onInstall, onClose}: {appVersion: string; connection: Connection | null; onInstall: () => Promise<BoardUpdateResult>; onClose: () => void}) {
  const build = connection?.daemon?.build;
  const buildLabel = build?.status === 'verified'
    ? `${build.commit?.slice(0, 12) ?? build.binarySha256?.slice(0, 12) ?? 'verified'}${build.dirty ? ' (modified)' : ''}`
    : build?.status ?? 'Not reported';
  const [check, setCheck] = useState<BoardUpdateCheck | null>(null);
  const [checking, setChecking] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [failure, setFailure] = useState('');
  const [success, setSuccess] = useState('');
  const request = useRef(0);
  const boardID = connection?.board.id ?? '';
  const activeTasks = connection?.daemon?.activeTasks ?? 0;
  const checkUpdate = useCallback(async () => {
    if (!boardID) return;
    const active = ++request.current;
    setChecking(true);
    setFailure('');
    try {
      const result = await api.checkBoardUpdate(boardID);
      if (request.current === active) setCheck(result);
    } catch (reason) {
      if (request.current === active) setFailure(friendlyError(String(reason)));
    } finally {
      if (request.current === active) setChecking(false);
    }
  }, [boardID]);
  useEffect(() => {
    request.current += 1;
    setCheck(null);
    setSuccess('');
    if (boardID) void checkUpdate();
    return () => {request.current += 1;};
  }, [boardID, connection?.daemon?.version, checkUpdate]);
  const install = async () => {
    if (installing || check?.status !== 'available' || activeTasks > 0) return;
    setInstalling(true);
    setFailure('');
    setSuccess('');
    try {
      const result = await onInstall();
      setCheck({status: 'current', installedVersion: result.installedVersion, availableVersion: result.installedVersion, message: 'This board is up to date.'});
      setSuccess(result.message);
    } catch (reason) {
      setFailure(friendlyError(String(reason)));
    } finally {
      setInstalling(false);
    }
  };
  const updateTone = failure ? 'failed' : check?.status === 'available' ? 'available' : check ? 'current' : 'checking';
  return <div className="modal-backdrop"><div className="modal about-modal"><div className="modal-header"><div><span className="modal-eyebrow">Hobot Code</span><h2>Version & updates</h2></div><button className="icon-button" title="Close" onClick={onClose} disabled={installing}><X size={18} /></button></div><div className="about-content"><div className="about-mark">H</div><InfoRow label="Studio" value={appVersion ? `v${appVersion}` : 'Unknown'} /><InfoRow label="Board service" value={connection?.daemon?.version ? `v${connection.daemon.version}` : 'Not connected'} /><InfoRow label="Board build" value={buildLabel} /><InfoRow label="Pi runtime" value={build?.piVersion ? `v${build.piVersion}` : 'Not reported'} /><InfoRow label="Compatibility" value={connection?.compatibility?.status ?? 'Not checked'} /><div className={`board-update-state ${updateTone}`}>{checking || installing ? <LoaderCircle size={17} className="spin" /> : failure ? <AlertTriangle size={17} /> : check?.status === 'available' ? <Download size={17} /> : <Check size={17} />}<span><strong>{installing ? `Installing v${check?.availableVersion ?? ''}` : checking ? 'Checking stable releases' : failure ? 'Update unavailable' : check?.status === 'available' ? `Version ${check.availableVersion} is ready` : check?.status === 'source-older' ? 'Installed version is newer' : check ? 'Up to date' : 'Connect a board to check'}</strong><small>{installing ? 'Downloading, verifying, installing, and reconnecting.' : failure || success || check?.message || 'Update status is available after connecting a board.'}</small></span></div>{activeTasks > 0 && <div className="update-blocked"><Info size={14} /><span>{activeTasks} active board task{activeTasks === 1 ? '' : 's'} must finish or stop before updating.</span></div>}<div className="update-actions">{check?.status === 'available' && <button className="primary-button" disabled={installing || checking || activeTasks > 0} onClick={() => void install()}>{installing ? <LoaderCircle size={14} className="spin" /> : <Download size={14} />}Update to v{check.availableVersion}</button>}<button className="secondary-button" disabled={installing || checking || !connection} onClick={() => void checkUpdate()}><RefreshCw size={14} className={checking ? 'spin' : ''} />Check again</button></div><div className="update-guidance"><ShieldCheck size={15} /><span><strong>Transactional board update</strong><small>Configuration and conversations stay in place. Downgrades are refused, releases are verified, and failed installs restore the previous runtime.</small></span></div><div className="command-list"><div><code>hobot update</code><CopyButton value="hobot update" /></div><div><code>hobot rollback</code><CopyButton value="hobot rollback" /></div></div><div className="modal-actions"><button className="primary-button" onClick={onClose} disabled={installing}>Done</button></div></div></div></div>;
}

function ExtensionCenterDialog({boardId, boardName, boardTarget, onClose}: {boardId: string; boardName: string; boardTarget: string; onClose: () => void}) {
  const [catalog, setCatalog] = useState<ExtensionCatalog | null>(null);
  const [loading, setLoading] = useState(true);
  const [failure, setFailure] = useState('');
  const [query, setQuery] = useState('');
  const [kind, setKind] = useState('all');
  const [revision, setRevision] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setFailure('');
    api.extensions(boardId).then((result) => {
      if (!cancelled) setCatalog(result);
    }).catch((reason) => {
      if (!cancelled) setFailure(friendlyError(String(reason)));
    }).finally(() => {
      if (!cancelled) setLoading(false);
    });
    return () => {cancelled = true;};
  }, [boardId, revision]);

  const target = boardTarget.trim().toLowerCase();
  const health = catalog ? extensionCatalogHealth(catalog) : null;
  const summary = catalog ? extensionCatalogSummary(catalog, target) : null;
  const entries = catalog ? filterExtensions(catalog.entries, query, kind) : [];
  const kinds = catalog ? ['all', ...Array.from(new Set(catalog.entries.map((entry) => entry.kind)))] : ['all'];

  return <div className="modal-backdrop"><section className="modal extension-center-modal" role="dialog" aria-modal="true" aria-labelledby="extension-center-title"><div className="modal-header"><div><span className="modal-eyebrow">{boardName}</span><h2 id="extension-center-title">Capabilities</h2></div><div className="modal-header-actions"><button className="icon-button" title="Refresh capabilities" onClick={() => setRevision((value) => value + 1)} disabled={loading}><RefreshCw size={16} className={loading ? 'spin' : ''} /></button><button className="icon-button" title="Close" onClick={onClose}><X size={18} /></button></div></div>
    {loading && !catalog ? <div className="extension-center-loading"><LoaderCircle size={18} className="spin" /><span>Reading the board catalog</span></div> : failure && !catalog ? <div className="extension-center-failure"><AlertTriangle size={18} /><span><strong>Catalog unavailable</strong><small>{failure}</small></span><button className="secondary-button" onClick={() => setRevision((value) => value + 1)}>Retry</button></div> : catalog && summary && health ? <>
      <div className="extension-overview"><div className={`extension-health ${health.healthy ? 'healthy' : 'warning'}`}>{health.healthy ? <ShieldCheck size={18} /> : <AlertTriangle size={18} />}<span><strong>{health.healthy ? 'Board-enforced catalog' : 'Catalog needs review'}</strong><small>{health.healthy ? `v${catalog.productVersion} · read-only inventory · ${target ? target.toUpperCase() : 'target unknown'}` : health.issues.join(' · ')}</small></span></div><div className="extension-stats"><span><strong>{summary.supported}</strong><small>For this board</small></span><span><strong>{summary.required}</strong><small>Required</small></span><span><strong>{summary.skills}</strong><small>Skills</small></span><span><strong>{summary.total}</strong><small>Total</small></span></div></div>
      {catalog.diagnostics && catalog.diagnostics.length > 0 && <div className="extension-sources" aria-label="Configured capability sources">{catalog.diagnostics.map((diagnostic) => <span key={diagnostic.source} className={`source-${diagnostic.status}`} title={diagnostic.message}>{extensionSourceIcon(diagnostic.status)}<strong>{extensionSourceLabel(diagnostic.source)}</strong><small>{extensionSourceStatus(diagnostic.status)}</small></span>)}</div>}
      <div className="extension-controls"><label className="extension-search"><Search size={14} /><input aria-label="Search capabilities" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search capabilities" /></label><div className="extension-kind-tabs" role="tablist" aria-label="Capability type">{kinds.map((value) => <button key={value} type="button" role="tab" aria-selected={kind === value} className={kind === value ? 'selected' : ''} onClick={() => setKind(value)}>{value === 'all' ? 'All' : extensionKindLabel(value)}</button>)}</div></div>
      <div className="extension-list">{entries.map((entry) => {
        const targetState = extensionTargetState(entry, target);
        const Icon = entry.kind === 'skill' ? Brain : entry.kind === 'provider' ? Bot : entry.kind === 'integration' ? Wrench : Box;
        const permissions = entry.permissions ?? [];
        const targets = entry.targets ?? [];
        const provides = entry.provides ?? [];
        const requires = entry.requires ?? [];
        return <article className={`extension-row extension-${entry.kind} extension-${targetState.state}`} key={entry.id}><div className="extension-row-icon"><Icon size={17} /></div><div className="extension-row-main"><div className="extension-row-heading"><strong>{entry.name}</strong><span className={`extension-state state-${targetState.state}`}>{targetState.label}</span></div><p>{entry.description}</p><div className="extension-meta"><span>{extensionKindLabel(entry.kind)}</span><code>{entry.version === 'configured' ? 'Configured' : `v${entry.version}`}</code><span>{entry.origin}</span><span>{entry.scope}</span></div>{permissions.length > 0 && <div className="extension-permissions"><ShieldCheck size={12} /><span>{permissions.map(extensionPermissionLabel).join(' · ')}</span></div>}<details className="extension-details"><summary>Technical details<ChevronRight className="details-chevron" size={12} /></summary><div><InfoRow label="ID" value={entry.id} mono copy={entry.id} /><InfoRow label="Runtime" value={entry.runtime} /><InfoRow label="Targets" value={targets.length ? targets.map((value) => value.toUpperCase()).join(', ') : 'All'} />{provides.length > 0 && <InfoRow label="Provides" value={provides.join(', ')} />}{requires.length > 0 && <InfoRow label="Requires" value={requires.join(', ')} />}</div></details></div></article>;
      })}{entries.length === 0 && <div className="extension-empty"><Search size={19} /><span>No matching capabilities</span></div>}</div>
      <footer className="extension-footer"><span><ShieldCheck size={13} />Execution: {catalog.policy.executionAuthority} · permissions: {catalog.policy.permissionAuthority}{catalog.capturedAt ? ` · ${relativeTime(catalog.capturedAt)}` : ''}</span><button className="primary-button" onClick={onClose}>Done</button></footer>
    </> : null}
  </section></div>;
}

function extensionPermissionLabel(permission: string) {
  const labels: Record<string, string> = {'model-network': 'Model network', workspace: 'Workspace', subprocess: 'Commands', 'rdk-devices': 'RDK devices', 'user-state': 'User state'};
  return labels[permission] ?? permission;
}

function extensionSourceLabel(source: string) {
  const labels: Record<string, string> = {providers: 'Providers', hooks: 'Hooks', lsp: 'LSP'};
  return labels[source] ?? source;
}

function extensionSourceStatus(status: string) {
  const labels: Record<string, string> = {ok: 'Inspected', missing: 'Not configured', invalid: 'Invalid', unsafe: 'Unsafe file', unreadable: 'Unreadable', truncated: 'Limited'};
  return labels[status] ?? status;
}

function extensionSourceIcon(status: string) {
  if (status === 'ok') return <Check size={11} />;
  if (status === 'missing') return <Info size={11} />;
  return <AlertTriangle size={11} />;
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

function DeleteDialog({target, busy, onClose, onDelete}: {target: {kind: 'conversation' | 'project'; label: string; taskIds: string[]; retainsWorktree?: boolean}; busy: boolean; onClose: () => void; onDelete: () => void}) {
  const project = target.kind === 'project';
  return <div className="modal-backdrop"><div className="modal confirm-modal"><div className="confirm-icon"><Trash2 size={18} /></div><h2>{project ? 'Remove project?' : 'Delete conversation?'}</h2><p>{project ? `This removes ${target.taskIds.length} conversation${target.taskIds.length === 1 ? '' : 's'} from ${target.label}.` : `This permanently removes ${target.label} from Hobot Code.`}</p><small>Running agents will stop. {target.retainsWorktree ? 'The isolated code workspace is retained on the board and can be reviewed or cleaned separately.' : 'Files in the board workspace will not be deleted.'}</small><div className="modal-actions"><button className="secondary-button" onClick={onClose} disabled={busy}>Cancel</button><button className="danger-button" onClick={onDelete} disabled={busy}>{busy ? <LoaderCircle size={15} className="spin" /> : <Trash2 size={15} />}Delete</button></div></div></div>;
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
  const recovery = taskRecovery(task);
  const taskAlerts = [recovery ? {tone: 'danger', label: recovery.message} : null, task?.logTruncated ? {tone: 'warning', label: 'Older task events were truncated.'} : null].filter(Boolean) as Array<{tone: string; label: string}>;
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
    {snapshot.workspaceWrites?.length ? <InspectorSection title="Workspaces being changed"><div className="hardware-leases">{snapshot.workspaceWrites.map((lease) => <div className="hardware-lease" key={`${lease.taskId}:${lease.cwd}`}><FilePenLine size={13} /><span><strong>{lease.cwd}</strong><small>{lease.taskId} · PID {lease.pid}</small></span></div>)}</div></InspectorSection> : null}
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
  const presentation = compatibilityPresentation(compatibility);
  const statusIcon = presentation.tone === 'healthy' ? <ShieldCheck size={15} /> : presentation.tone === 'danger' ? <XCircle size={15} /> : <AlertTriangle size={15} />;
  return <InspectorSection title="Compatibility">
    <div className={`compatibility-summary compatibility-${presentation.tone}`}>{statusIcon}<div><span>{presentation.label}</span><strong>{presentation.title}</strong><small>{presentation.description}</small></div></div>
    {presentation.action && <div className={`compatibility-action ${presentation.tone}`}><ChevronRight size={13} /><span>{presentation.action}</span></div>}
    <details className="compatibility-details"><summary>Technical details<ChevronRight className="details-chevron" size={13} /></summary><div><InfoRow label="Studio / board" value={`${compatibility.appVersion} / ${compatibility.agentdVersion}`} /><InfoRow label="Protocol / events" value={`${compatibility.protocol} / ${compatibility.eventSchema}`} /><InfoRow label="Target" value={compatibilityTargetLabel(compatibility)} />{compatibility.issues.length > 0 && <div className="compatibility-issues">{compatibility.issues.map((issue) => <div key={issue.code} className={issue.severity}><AlertTriangle size={13} /><span><strong>{issue.message}</strong>{issue.action && issue.action !== presentation.action && <small>{issue.action}</small>}</span></div>)}</div>}</div></details>
  </InspectorSection>;
}

function InspectorSection({title, children}: {title: string; children: ReactNode}) { return <section className="inspector-section"><h3>{title}</h3>{children}</section>; }

function TaskSandboxInspector({task}: {task: Task}) {
  const sandbox = task.sandbox;
  if (!sandbox) return null;
  return <InspectorSection title="Agent boundary"><InfoRow label="Profile" value={`${task.sandboxMode || sandbox.effective} · ${sandbox.backend}`} /><InfoRow label="File writes" value={sandbox.filesystemRestricted ? 'Restricted' : 'Host access'} /><InfoRow label="Devices" value={sandbox.devicesRestricted ? 'Minimal devices' : task.sandboxMode === 'off' ? 'Host access' : 'Board hardware'} /><InfoRow label="Privileges" value={sandbox.capabilitiesDropped ? 'Dropped' : 'Host privileges'} /><InfoRow label="Network" value={sandbox.networkRestricted ? 'Restricted' : 'Shared for model access'} />{sandbox.reason && <div className="deployment-summary">{sandbox.reason}</div>}</InspectorSection>;
}
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
  if (/background task limit reached.*all agents are currently working/i.test(message)) return 'This older board service cannot queue tasks. Update Hobot Code on the board, or stop a working agent.';
  return message;
}

export default App;
