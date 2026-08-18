import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import type {FormEvent, ReactNode, UIEvent} from 'react';
import ReactMarkdown from 'react-markdown';
import 'katex/dist/katex.min.css';
import {
  Activity, AlertTriangle, ArrowDown, ArrowUp, Bot, Box, Brain, CalendarClock, Check, ChevronDown,
  ChevronRight, Clipboard, CornerDownRight, Cpu, FilePenLine, Folder,
  GitBranch, ListTodo, LoaderCircle, MessageSquare,
  Download, FileDiff, Gauge, Info, KeyRound, Monitor, Moon, MoreHorizontal, Package, Palette, PanelRight, Paperclip, Plus, RefreshCw, Search, Server, ShieldCheck, Sun,
  Play, Square, SquareTerminal, Trash2, Wrench, X, XCircle, Zap,
} from 'lucide-react';
import {api, isMock} from './api';
import type {TaskWatchStatus} from './api';
import {composerIsBlocked, composerMode, shouldCancelTurnShortcut, shouldSubmitComposer, terminalStatuses, turnCancellationMode} from './composer-policy.js';
import {buildConversation, elapsedLabel, eventRetentionPresentation, recentEventsAfter} from './conversation-model.js';
import {approvalPresentation, approvalResponse} from './approval-model.js';
import {acceleratorMemoryMetrics, activeDDRBandwidth, boardHealth, bpuCoreLabel, bpuFrequency, bpuTemperature, bpuUnavailableReason, bpuUtilization, durationLabel, formatBytes, orphanedIONNotice, percentLabel, systemResourceMetrics} from './board-health.js';
import {arrangeTasks, groupTasksByProject} from './project-model.js';
import {markdownRehypePlugins, markdownRemarkPlugins} from './markdown-config.js';
import {effectiveModel as resolveEffectiveModel, modelAcceptsImages} from './model-capabilities.js';
import {currentModelHealth as resolveCurrentModelHealth} from './model-health.js';
import {currentModelConformance as resolveCurrentModelConformance} from './model-conformance.js';
import {currentModelProbe, currentModelQualification, modelReadinessPresentation, qualificationEvidenceNotice, qualificationExpirations, qualificationLayer} from './model-readiness.js';
import {currentModelRDKMatrix as resolveCurrentModelRDKMatrix, rdkProfileEvidenceLabel, rdkProfileState} from './rdk-profile-matrix.js';
import {rdkWorkflows} from './rdk-workflows.js';
import {deploymentCanStart, deploymentCompatibilityLabel, deploymentPhaseLabel, deploymentProfileFor, preferredDeploymentArtifact} from './deployment-model.js';
import {shouldToggleMaximise} from './titlebar-policy.js';
import {isCurrentRequest, isCurrentTarget, watchRetryDelay, watchStatusLabel} from './async-policy.js';
import {taskAttention} from './task-notifications.js';
import {taskRecovery, taskRecoveryActionAvailable} from './task-recovery.js';
import {compatibilityPresentation, compatibilityTargetLabel} from './compatibility-presentation.js';
import {workspaceChangeLabel, workspaceChangeSummary, workspaceDeliverySummary, workspaceDiffLines} from './workspace-changes.js';
import {extensionCatalogHealth, extensionCatalogSummary, extensionKindLabel, extensionTargetState, filterExtensions} from './extension-center.js';
import {supportBundlePresentation} from './support-diagnostics.js';
import {accessModePresentation} from './access-mode.js';
import {friendlyError} from './friendly-error.js';
import {includedModelSummary, includedProviderGroups} from './provider-catalog.js';
import {applyTheme, readThemePreference, resolveTheme, saveThemePreference} from './appearance-theme.js';
import type {ThemePreference} from './appearance-theme.js';
import type {AssistantConversationItem, ToolActivity, UserConversationItem} from './conversation-model.js';
import type {AddManagedProviderRequest, Approval, Board, BoardUpdateCheck, BoardUpdateResult, BPUBenchmarkRequest, BPUBenchmarkResult, BPUModelInfo, BPUTensorDesc, Connection, DeploymentInspection, DeploymentStatus, DiagnosticReport, EventPage, ExtensionCatalog, ImageContent, ManagedProvider, ModelConformance, ModelHealth, ModelOption, ModelQualification, ModelRDKMatrix, ModelRDKProbe, ModelRDKProfileStatus, ModelRuntimeProbe, ProviderMutationResult, Schedule, StartDeploymentRequest, StudioUpdateCheck, SupportBundle, SystemSnapshot, Task, TaskEvent, WorkspaceChanges, WorkspaceDelivery, WorkspaceIsolation, WorkspaceListing} from './types';

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
  const [modelRuntimeProbe, setModelRuntimeProbe] = useState<ModelRuntimeProbe | null>(null);
  const [modelRDKProbe, setModelRDKProbe] = useState<ModelRDKProbe | null>(null);
  const [modelRDKMatrix, setModelRDKMatrix] = useState<ModelRDKMatrix | null>(null);
  const [modelQualification, setModelQualification] = useState<ModelQualification | null>(null);
  const [modelQualificationStage, setModelQualificationStage] = useState<'runtime' | 'rdk' | ''>('');
  const [modelQualificationProfile, setModelQualificationProfile] = useState('');
  const [modelReadinessError, setModelReadinessError] = useState('');
  const [showModelReadiness, setShowModelReadiness] = useState(false);
  const [showAccessSettings, setShowAccessSettings] = useState(false);
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
	const [showSchedules, setShowSchedules] = useState(false);
  const [showProviders, setShowProviders] = useState(false);
  const [supportBundle, setSupportBundle] = useState<SupportBundle | null>(null);
  const [diagnostics, setDiagnostics] = useState<DiagnosticReport | null>(null);
  const [diagnosticsError, setDiagnosticsError] = useState('');
  const [showDiagnostics, setShowDiagnostics] = useState(false);
  const [showBPUBenchmark, setShowBPUBenchmark] = useState(false);
  const [diagnosticsLoading, setDiagnosticsLoading] = useState(false);
  const [showChanges, setShowChanges] = useState(false);

	const [workspaceInspection, setWorkspaceInspection] = useState<{taskId: string; loading: boolean; result?: WorkspaceIsolation} | null>(null);
  const [appVersion, setAppVersion] = useState('');
  const [unreadTasks, setUnreadTasks] = useState<Set<string>>(new Set());
  const [showInspector, setShowInspector] = useState(false);
  const [themePreference, setThemePreference] = useState<ThemePreference>(() => readThemePreference(window.localStorage));
  const [systemPrefersDark, setSystemPrefersDark] = useState(() => window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? true);
  const [showAppearance, setShowAppearance] = useState(false);
  const [collapsedProjects, setCollapsedProjects] = useState<Set<string>>(new Set());
  const [openMenu, setOpenMenu] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<{kind: 'conversation' | 'project'; label: string; taskIds: string[]; retainsWorktree?: boolean} | null>(null);
  const [renamingTask, setRenamingTask] = useState(false);
  const [renameValue, setRenameValue] = useState('');
  const [watchRevision, setWatchRevision] = useState(0);
  const [watchStatus, setWatchStatus] = useState<TaskWatchStatus | null>(null);
  const [eventRetention, setEventRetention] = useState<Pick<EventPage, 'retainedFrom' | 'retainedThrough' | 'latestSequence' | 'historyTruncated' | 'cursorExpired'> | null>(null);
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
  const modelQualificationRequest = useRef(0);
  const connectionRequest = useRef(0);
  const connectionTarget = useRef('');
  const watchRetryAttempt = useRef(0);
  const watchRetryTimer = useRef<number | null>(null);
  const taskStatusHistory = useRef(new Map<string, string>());
  const appearanceRef = useRef<HTMLDivElement>(null);

  const boardId = connection?.board.id ?? '';
  const resolvedTheme = resolveTheme(themePreference, systemPrefersDark);
  const AppearanceIcon = themePreference === 'system' ? Monitor : resolvedTheme === 'dark' ? Moon : Sun;

  useEffect(() => {
    const media = window.matchMedia?.('(prefers-color-scheme: dark)');
    if (!media) return;
    const update = (event: MediaQueryListEvent | MediaQueryList) => setSystemPrefersDark(event.matches);
    update(media);
    media.addEventListener?.('change', update);
    return () => media.removeEventListener?.('change', update);
  }, []);

  useEffect(() => {
    applyTheme(document.documentElement, themePreference, systemPrefersDark);
    saveThemePreference(window.localStorage, themePreference);
    if (themePreference === 'system') window.runtime?.WindowSetSystemDefaultTheme?.();
    else if (themePreference === 'light') window.runtime?.WindowSetLightTheme?.();
    else window.runtime?.WindowSetDarkTheme?.();
  }, [systemPrefersDark, themePreference]);

  useEffect(() => {
    if (!showAppearance) return;
    const closeOnPointer = (event: PointerEvent) => {
      if (!appearanceRef.current?.contains(event.target as Node)) setShowAppearance(false);
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setShowAppearance(false);
    };
    document.addEventListener('pointerdown', closeOnPointer);
    document.addEventListener('keydown', closeOnEscape);
    return () => {
      document.removeEventListener('pointerdown', closeOnPointer);
      document.removeEventListener('keydown', closeOnEscape);
    };
  }, [showAppearance]);

  useEffect(() => {
    const target = boardId;
    if (!target || connectionState !== 'online' || !connection?.capabilities?.capabilities.includes('diagnostics.inspect.v1')) {
      setDiagnostics(null);
      setDiagnosticsError('');
      return;
    }
    setDiagnosticsError('');
    api.diagnostics(target).then((report) => {
      if (activeBoardId.current === target) {
        setDiagnostics(report);
        setDiagnosticsError('');
      }
    }).catch((reason) => {
      if (activeBoardId.current === target) {
        setDiagnostics(null);
        setDiagnosticsError(friendlyError(String(reason)));
      }
    });
  }, [boardId, connection?.capabilities?.capabilities, connectionState]);

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
    modelQualificationRequest.current += 1;
    setCheckingModel(false);
    setVerifyingModel(false);
    setModelHealth(null);
    setModelConformance(null);
    setModelRuntimeProbe(null);
    setModelRDKProbe(null);
    setModelRDKMatrix(null);
    setModelQualification(null);
    setModelQualificationStage('');
    setModelQualificationProfile('');
    setModelReadinessError('');
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
        setEventRetention((current) => ({
          retainedFrom: status.retainedFrom ?? current?.retainedFrom,
          retainedThrough: status.retainedThrough ?? current?.retainedThrough,
          latestSequence: status.latestSequence ?? current?.latestSequence,
          historyTruncated: Boolean(status.historyTruncated || current?.historyTruncated),
          cursorExpired: Boolean(status.cursorExpired || current?.cursorExpired),
        }));
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
      setEventRetention(null);
      setEventsLoading(false);
      return;
    }
    let active = true;
    setEventsLoading(true);
    setEventRetention(null);
    const initialAfter = recentEventsAfter(selectedTask.lastSequence);
    api.events(boardId, selectedTask.id, initialAfter, 400).then((page) => {
      if (!active) return;
      setEvents(page.events ?? []);
      setEventRetention({
        retainedFrom: page.retainedFrom,
        retainedThrough: page.retainedThrough,
        latestSequence: page.latestSequence,
        historyTruncated: Boolean(page.historyTruncated || selectedTask.logTruncated),
        cursorExpired: Boolean(page.cursorExpired),
      });
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
	const model = resolveEffectiveModel(models, selectedTask?.model ?? '');
	const targetBoard = boardId;
	const selection = model ? `${model.provider}/${model.id}` : '';
	const request = ++modelQualificationRequest.current;
	setModelQualification(null);
	setModelRDKMatrix(null);
	if (!targetBoard || !selection || connectionState !== 'online') return;
	const capabilities = connection?.capabilities?.capabilities ?? [];
	const reads: Promise<void>[] = [];
	if (capabilities.includes('models.qualification.v1')) reads.push(api.modelQualification(targetBoard, selection).then((result) => {
		if (modelQualificationRequest.current === request && activeBoardId.current === targetBoard) setModelQualification(result);
	}));
	if (capabilities.includes('models.rdk-matrix.v1')) reads.push(api.modelRDKMatrix(targetBoard, selection).then((result) => {
		if (modelQualificationRequest.current === request && activeBoardId.current === targetBoard) setModelRDKMatrix(result);
	}));
	for (const read of reads) void read.catch((reason) => {
		if (modelQualificationRequest.current === request && activeBoardId.current === targetBoard) setModelReadinessError(friendlyError(String(reason)));
	});
  }, [boardId, connection?.capabilities?.capabilities, connectionState, models, selectedTask?.model]);

  useEffect(() => {
	if (!modelQualification || modelQualification.state === 'stale' || modelQualification.state === 'untested') return;
	const expirations = [modelQualification.health?.expiresAt, modelQualification.conformance?.expiresAt]
		.filter((value): value is string => Boolean(value))
		.map((value) => Date.parse(value))
		.filter((value) => Number.isFinite(value) && value > Date.now());
	if (expirations.length === 0) return;
	const wait = Math.min(...expirations) - Date.now();
	const timer = window.setTimeout(() => setModelQualification((current) => {
		if (!current || current.provider !== modelQualification.provider || current.model !== modelQualification.model || current.updatedAt !== modelQualification.updatedAt) return current;
		const expiredLayers = qualificationExpirations(current);
		const next = {...current, expiredLayers};
		const currentEvidence = qualificationLayer(next, 'route', current.health) || qualificationLayer(next, 'protocol', current.conformance) || qualificationLayer(next, 'runtime', current.runtime) || qualificationLayer(next, 'rdk', current.rdk);
		return {...next, state: currentEvidence ? current.state : 'expired'};
	}), Math.max(0, wait + 10));
	return () => window.clearTimeout(timer);
  }, [modelQualification]);

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
  const retentionNotice = useMemo(() => eventRetentionPresentation(eventRetention), [eventRetention]);
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
  const modelPickerValue = models.some((model) => `${model.provider}/${model.id}` === selectedModel) ? selectedModel : '';
  const effectiveModel = resolveEffectiveModel(models, selectedModel);
  const qualificationForModel = currentModelQualification(modelQualification, effectiveModel);
	const currentModelHealth = resolveCurrentModelHealth(modelHealth, effectiveModel) ?? qualificationLayer(qualificationForModel, 'route', qualificationForModel?.health);
	const currentModelConformance = resolveCurrentModelConformance(modelConformance, effectiveModel) ?? qualificationLayer(qualificationForModel, 'protocol', qualificationForModel?.conformance);
	const currentModelRuntimeProbe = currentModelProbe(modelRuntimeProbe, effectiveModel) ?? qualificationLayer(qualificationForModel, 'runtime', qualificationForModel?.runtime);
	const currentModelRDKProbe = currentModelProbe(modelRDKProbe, effectiveModel) ?? qualificationLayer(qualificationForModel, 'rdk', qualificationForModel?.rdk);
	const currentModelRDKMatrix = resolveCurrentModelRDKMatrix(modelRDKMatrix, effectiveModel);
	const effectiveQualificationState = qualificationForModel && qualificationExpirations(qualificationForModel).length > 0 && !currentModelHealth && !currentModelConformance && !currentModelRuntimeProbe && !currentModelRDKProbe ? 'expired' : qualificationForModel?.state;
	const modelReadiness = modelReadinessPresentation({health: currentModelHealth, conformance: currentModelConformance, runtime: currentModelRuntimeProbe, rdk: currentModelRDKProbe, evidenceState: effectiveQualificationState, running: checkingModel || verifyingModel || Boolean(modelQualificationStage)});
  const imageInputSupported = modelAcceptsImages(models, selectedModel);
  const selectedPermissionMode = selectedTask?.permissionMode ?? 'ask';
  const selectedSandboxMode = selectedTask?.sandboxMode ?? (selectedPermissionMode === 'review' ? 'review' : 'workspace');
  const selectedNetworkMode = selectedTask?.networkMode ?? 'shared';
  const accessPresentation = accessModePresentation({permissionMode: selectedPermissionMode, sandboxMode: selectedSandboxMode, networkMode: selectedNetworkMode});
  const canCreateBlankSideTask = Boolean(connection?.capabilities?.capabilities.includes('tasks.fork.deferred-prompt.v1'));
  const canChangeModel = Boolean(connectionState === 'online' && selectedTask && (draftSelected || selectedTask.status === 'idle' || terminalStatuses.has(selectedTask.status)) && !busy && !modelQualificationStage && !checkingModel && !verifyingModel);
  const canChangePermissions = canChangeModel;
  const canChangeSandbox = Boolean(connection?.capabilities?.capabilities.includes('tasks.sandbox.v1') && selectedTask && (draftSelected || selectedTask.status === 'queued' || terminalStatuses.has(selectedTask.status)) && !busy && connectionState === 'online');
  const canChangeNetwork = Boolean(connection?.capabilities?.capabilities.includes('tasks.network.v1') && selectedTask && (draftSelected || selectedTask.status === 'queued' || terminalStatuses.has(selectedTask.status)) && !busy && connectionState === 'online');
  const canStopBoundaryWorker = Boolean(selectedTask?.status === 'idle' && connectionState === 'online');
  const cancellationMode = turnCancellationMode(selectedTask?.status);
  const canCancelCurrentWork = Boolean(cancellationMode);
  const latestConversationItem = conversation[conversation.length - 1];
  const activityStart = optimisticPrompt && optimisticPrompt.taskId === selectedTask?.id
    ? optimisticPrompt.time
    : latestConversationItem?.kind === 'user' ? latestConversationItem.time : selectedTask?.updatedAt;

  useEffect(() => {
    if (!selectedTask || !cancellationMode || busy || connectionState !== 'online') return;
    const cancelOnEscape = (event: KeyboardEvent) => {
      if (!shouldCancelTurnShortcut(event.key, event.isComposing, event.repeat, selectedTask.status)) return;
      const target = event.target instanceof Element ? event.target : null;
      const textControl = target?.closest('input, textarea, select, [contenteditable="true"]');
      if (textControl && textControl !== composerRef.current) return;
      if (document.querySelector('.modal-backdrop, .appearance-menu, .row-menu')) return;
      event.preventDefault();
      event.stopPropagation();
      void cancelCurrentWork();
    };
    document.addEventListener('keydown', cancelOnEscape, true);
    return () => document.removeEventListener('keydown', cancelOnEscape, true);
  }, [busy, cancellationMode, connectionState, selectedTask?.id, selectedTask?.status]);

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
		networkMode: connection?.capabilities?.capabilities.includes('tasks.network.v1') ? selectedNetworkMode : undefined,
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

  async function cancelCurrentWork() {
    if (!selectedTask || !boardId) return;
    const mode = turnCancellationMode(selectedTask.status);
    if (!mode) return;
    const sourceBoardId = boardId;
    const sourceTaskId = selectedTask.id;
    setBusy(true);
    setError('');
    try {
      if (mode === 'stop') await api.stopTask(boardId, selectedTask.id);
      else await api.abortTask(boardId, selectedTask.id);
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
    modelQualificationRequest.current += 1;
    setCheckingModel(false);
    setVerifyingModel(false);
    setModelHealth(null);
    setModelConformance(null);
    setModelRuntimeProbe(null);
    setModelRDKProbe(null);
    setModelRDKMatrix(null);
    setModelQualification(null);
    setModelQualificationStage('');
    setModelQualificationProfile('');
    setModelReadinessError('');
    if (attachments.length > 0 && nextModel?.capabilities?.imageInput !== true) {
      setAttachments([]);
      setNotice(`${nextModel?.name || modelId} does not support image input. Attachments were removed.`);
    }
	if (draftSelected) {
	  const networkMode = nextModel?.modelOnly !== true && selectedTask.networkMode === 'model-only' ? 'shared' : selectedTask.networkMode ?? 'shared';
	  if (networkMode !== selectedTask.networkMode) setNotice('This provider is not yet supported by Model only. Network was changed to shared.');
	  setSelectedTask({...selectedTask, model: value, networkMode});
      return;
    }
    const sourceTaskId = selectedTask.id;
    setBusy(true);
    setError('');
    try {
      const networkMode = nextModel?.modelOnly !== true && selectedTask.networkMode === 'model-only' ? 'shared' : selectedTask.networkMode ?? 'shared';
	  if (networkMode !== selectedTask.networkMode) {
		await api.setNetworkMode(boardId, selectedTask.id, networkMode);
		setNotice('This provider is not yet supported by Model only. Network was changed to shared.');
	  }
      await api.setModel(boardId, selectedTask.id, provider, modelId);
	  if (isCurrentTarget(boardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setSelectedTask({...selectedTask, model: value, networkMode});
	  if (activeBoardId.current === boardId) setTasks((current) => current.map((task) => task.id === selectedTask.id ? {...task, model: value, networkMode} : task));
    } catch (reason) {
      if (isCurrentTarget(boardId, sourceTaskId, activeBoardId.current, selectedTaskId.current)) setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  async function checkModelHealth() {
	if (!boardId || !effectiveModel || effectiveModel.provider !== 'drobotics' || checkingModel || verifyingModel || modelQualificationStage || !connection?.capabilities?.capabilities.includes('models.health.v1')) return;
    const targetBoard = boardId;
    const request = ++modelHealthRequest.current;
    setCheckingModel(true);
    setModelReadinessError('');
    setError('');
    try {
      const result = await api.modelHealth(targetBoard, `${effectiveModel.provider}/${effectiveModel.id}`, Boolean(currentModelHealth));
      if (modelHealthRequest.current === request && activeBoardId.current === targetBoard) {
		setModelHealth(result);
		void refreshModelQualification(targetBoard, `${effectiveModel.provider}/${effectiveModel.id}`);
	  }
    } catch (reason) {
      if (modelHealthRequest.current === request && activeBoardId.current === targetBoard) {
        const message = friendlyError(String(reason));
        setModelReadinessError(message);
        setError(message);
      }
    } finally {
      if (modelHealthRequest.current === request) setCheckingModel(false);
    }
  }

  async function verifyModelConformance() {
	if (!boardId || !effectiveModel || effectiveModel.provider !== 'drobotics' || checkingModel || verifyingModel || modelQualificationStage || !connection?.capabilities?.capabilities.includes('models.conformance.v1')) return;
    const targetBoard = boardId;
    const request = ++modelVerificationRequest.current;
    setVerifyingModel(true);
    setModelReadinessError('');
    setError('');
    try {
      const result = await api.modelConformance(targetBoard, `${effectiveModel.provider}/${effectiveModel.id}`, Boolean(currentModelConformance));
      if (modelVerificationRequest.current === request && activeBoardId.current === targetBoard) {
		setModelConformance(result);
		void refreshModelQualification(targetBoard, `${effectiveModel.provider}/${effectiveModel.id}`);
	  }
    } catch (reason) {
      if (modelVerificationRequest.current === request && activeBoardId.current === targetBoard) {
        const message = friendlyError(String(reason));
        setModelReadinessError(message);
        setError(message);
      }
    } finally {
      if (modelVerificationRequest.current === request) setVerifyingModel(false);
    }
  }

  async function probeModelRuntime() {
    if (!boardId || !effectiveModel || checkingModel || verifyingModel || modelQualificationStage || !connection?.capabilities?.capabilities.includes('models.runtime-probe.v1')) return;
    const targetBoard = boardId;
    const targetModel = `${effectiveModel.provider}/${effectiveModel.id}`;
    const request = ++modelQualificationRequest.current;
    setModelQualificationStage('runtime');
    setModelReadinessError('');
    setError('');
    try {
      const result = await api.modelRuntimeProbe(targetBoard, targetModel);
      if (modelQualificationRequest.current === request && activeBoardId.current === targetBoard) {
		setModelRuntimeProbe(result);
		void refreshModelQualification(targetBoard, targetModel);
	  }
    } catch (reason) {
      if (modelQualificationRequest.current === request && activeBoardId.current === targetBoard) {
        const message = friendlyError(String(reason));
        setModelReadinessError(message);
        setError(message);
      }
    } finally {
      if (modelQualificationRequest.current === request) setModelQualificationStage('');
    }
  }

  async function probeModelRDK(profile = 'read-only-rdk-diagnostic-v1') {
    if (!boardId || !effectiveModel || checkingModel || verifyingModel || modelQualificationStage || !connection?.capabilities?.capabilities.includes('models.rdk-probe.v1')) return;
    const targetBoard = boardId;
    const targetModel = `${effectiveModel.provider}/${effectiveModel.id}`;
    const request = ++modelQualificationRequest.current;
    setModelQualificationStage('rdk');
    setModelQualificationProfile(profile);
    setModelReadinessError('');
    setError('');
    try {
      const result = await api.modelRDKProbe(targetBoard, targetModel, profile);
      if (modelQualificationRequest.current === request && activeBoardId.current === targetBoard) {
		if (profile === 'read-only-rdk-diagnostic-v1') setModelRDKProbe(result);
		void refreshModelQualification(targetBoard, targetModel);
	  }
    } catch (reason) {
      if (modelQualificationRequest.current === request && activeBoardId.current === targetBoard) {
        const message = friendlyError(String(reason));
        setModelReadinessError(message);
        setError(message);
      }
    } finally {
		if (modelQualificationRequest.current === request) {
			setModelQualificationStage('');
			setModelQualificationProfile('');
		}
    }
  }

  async function refreshModelQualification(targetBoard: string, targetModel: string) {
	const capabilities = connection?.capabilities?.capabilities ?? [];
	const reads: Promise<void>[] = [];
	if (capabilities.includes('models.qualification.v1')) reads.push(api.modelQualification(targetBoard, targetModel).then((result) => {
		if (activeBoardId.current === targetBoard) setModelQualification(result);
	}));
	if (capabilities.includes('models.rdk-matrix.v1')) reads.push(api.modelRDKMatrix(targetBoard, targetModel).then((result) => {
		if (activeBoardId.current === targetBoard) setModelRDKMatrix(result);
	}));
	for (const read of reads) void read.catch((reason) => {
		if (activeBoardId.current === targetBoard) setModelReadinessError(friendlyError(String(reason)));
	});
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

  async function changeNetworkMode(mode: string) {
    if (!selectedTask || !boardId || !canChangeNetwork) return;
	  if (mode !== 'shared' && selectedSandboxMode === 'off') {
		setError('Restricted networking requires an active OS sandbox.');
      return;
    }
    if (draftSelected) {
      setSelectedTask({...selectedTask, networkMode: mode as Task['networkMode']});
      return;
    }
    const sourceTaskId = selectedTask.id;
    setBusy(true);
    setError('');
    try {
      const task = await api.setNetworkMode(boardId, selectedTask.id, mode);
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
	  const draftModel = models.find((model) => model.default) ?? models[0];
	  const draftNetworkMode = draftModel?.modelOnly === true && connection?.capabilities?.sandbox?.networkModes?.includes('model-only') ? 'model-only' : 'shared';
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
	      model: draftModel ? `${draftModel.provider}/${draftModel.id}` : '',
      permissionMode: 'ask',
	  sandboxMode: 'workspace',
		  networkMode: draftNetworkMode,
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
    setSupportBundle(null);
    try {
      const bundle = await api.saveSupportBundle(boardId);
      if (bundle.path) setSupportBundle(bundle);
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  async function openDiagnostics() {
    if (!boardId || diagnosticsLoading) return;
    const target = boardId;
    setShowDiagnostics(true);
    setDiagnosticsLoading(true);
    setDiagnosticsError('');
    try {
      const report = await api.diagnostics(target);
      if (activeBoardId.current === target) setDiagnostics(report);
    } catch (reason) {
      if (activeBoardId.current === target) {
        setDiagnostics(null);
        setDiagnosticsError(friendlyError(String(reason)));
      }
    } finally {
      setDiagnosticsLoading(false);
    }
  }

  async function repairDiagnostic(action: string) {
    if (!boardId || diagnosticsLoading || !window.confirm('Apply this bounded repair on the board? Active Agent work will never be interrupted.')) return;
    setDiagnosticsLoading(true);
    setError('');
    try {
      const report = await api.repairDiagnostics(boardId, action, true);
      setDiagnostics(report);
      setNotice('Board readiness was checked again after the repair.');
      if (action === 'restart-daemon') setConnection(await api.refreshBoard(boardId));
    } catch (reason) {
      setError(String(reason));
    } finally {
      setDiagnosticsLoading(false);
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
        <div className="appearance-control" ref={appearanceRef} data-titlebar-no-drag>
          <button className={`icon-button ${showAppearance ? 'active' : ''}`} title={`Appearance: ${themePreference === 'system' ? 'System' : themePreference === 'light' ? 'Light' : 'Dark'}`} aria-label="Appearance" aria-haspopup="menu" aria-expanded={showAppearance} onClick={() => setShowAppearance((value) => !value)}><AppearanceIcon size={16} /></button>
          {showAppearance && <div className="appearance-menu" role="menu" aria-label="Appearance">
            <button type="button" role="menuitemradio" aria-checked={themePreference === 'system'} className={themePreference === 'system' ? 'selected' : ''} onClick={() => {setThemePreference('system'); setShowAppearance(false);}}><Monitor size={15} /><span>System</span>{themePreference === 'system' && <Check size={14} />}</button>
            <button type="button" role="menuitemradio" aria-checked={themePreference === 'light'} className={themePreference === 'light' ? 'selected' : ''} onClick={() => {setThemePreference('light'); setShowAppearance(false);}}><Sun size={15} /><span>Light</span>{themePreference === 'light' && <Check size={14} />}</button>
            <button type="button" role="menuitemradio" aria-checked={themePreference === 'dark'} className={themePreference === 'dark' ? 'selected' : ''} onClick={() => {setThemePreference('dark'); setShowAppearance(false);}}><Moon size={15} /><span>Dark</span>{themePreference === 'dark' && <Check size={14} />}</button>
          </div>}
        </div>
        <button className="version-button" title="Version and updates" onClick={() => setShowAbout(true)}>{appVersion ? `v${appVersion}` : <LoaderCircle size={12} className="spin" />}</button>
        {connection?.capabilities?.capabilities.includes('extensions.catalog.v1') && <button className="icon-button" title="Capabilities" disabled={connectionState !== 'online'} onClick={() => setShowExtensions(true)}><Box size={16} /></button>}
		{connection?.capabilities?.capabilities.includes('schedules.v1') && <button className="icon-button" title="Schedules" disabled={connectionState !== 'online'} onClick={() => setShowSchedules(true)}><CalendarClock size={16} /></button>}
        {connection?.capabilities?.capabilities.includes('providers.manage.v1') && <button className="icon-button" title="Model providers" disabled={connectionState !== 'online'} onClick={() => setShowProviders(true)}><KeyRound size={16} /></button>}
        {connection?.capabilities?.capabilities.includes('diagnostics.inspect.v1') && <button className={`icon-button diagnostic-status ${diagnostics?.status ?? ''}`} title="Board readiness" disabled={connectionState !== 'online'} onClick={() => void openDiagnostics()}>{diagnosticsLoading ? <LoaderCircle size={16} className="spin" /> : <Activity size={16} />}</button>}
        {connectionState === 'online' && <button className="icon-button" title="BPU Benchmark & Model Inspector" onClick={() => setShowBPUBenchmark(true)}><Gauge size={16} /></button>}
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
			<div className="task-title-block"><div className="task-title-line">{renamingTask ? <form className="title-editor" onSubmit={(event) => {event.preventDefault(); void renameSelectedTask();}}><input value={renameValue} maxLength={64} autoFocus onChange={(event) => setRenameValue(event.target.value)} onBlur={() => void renameSelectedTask()} onKeyDown={(event) => {if (event.key === 'Escape') {setRenameValue(selectedTask.name); setRenamingTask(false);}}} /></form> : <><h1 title="Double-click to rename" onDoubleClick={() => beginRename(selectedTask)}>{selectedTask.name}</h1><button className="title-edit" title="Rename conversation" onClick={() => beginRename(selectedTask)}><FilePenLine size={13} /></button></>}<span className={`status status-${awaitingFirstPrompt ? 'idle' : selectedTask.status}`}>{awaitingFirstPrompt ? 'Ready' : statusLabel[selectedTask.status] ?? selectedTask.status}</span>{selectedTask.currentActivity && <span className="task-activity" title="Public coordination status shared with related Agents">{selectedTask.currentActivity}</span>}{selectedTask.workspaceMode === 'worktree' && <span className="workspace-mode-badge">Isolated</span>}{watchStatus && <span className={`stream-status stream-${watchStatus.state}`} role="status" title={watchStatus.message}><RefreshCw size={11} className="spin" />{watchStatusLabel(watchStatus)}</span>}</div><span className="workspace-path">{selectedTask.projectCwd || selectedTask.cwd}</span></div>
            <div className="task-actions">
              {!draftSelected && connection?.capabilities?.capabilities.includes('workspaces.changes.v1') && <button className="secondary-button changes-button" title="Review current workspace changes" onClick={() => setShowChanges(true)} disabled={connectionState !== 'online'}><FileDiff size={15} />Changes</button>}
              {!draftSelected && <button className="secondary-button side-task-button" title={!selectedTask.sessionFile ? 'Side Agent is available after the first response' : !canCreateBlankSideTask ? 'Update Hobot Code on the board to open blank Side Agent conversations' : 'Open an independent conversation from this context'} onClick={() => void createSideTask()} disabled={busy || !selectedTask.sessionFile || !canCreateBlankSideTask}><GitBranch size={15} />Side Agent</button>}
              {terminalStatuses.has(selectedTask.status) && !awaitingFirstPrompt && <button className="secondary-button" onClick={() => composerRef.current?.focus()}><RefreshCw size={14} />{selectedComposerMode === 'resume' ? 'Resume' : 'New session'}</button>}
            </div>
          </div>

          <div className="conversation" ref={timelineRef} onScroll={onTimelineScroll} aria-label="Conversation">
            <div className="conversation-inner">
              {eventsLoading && events.length === 0 && <div className="loading-conversation"><LoaderCircle size={18} className="spin" /><span>Loading conversation</span></div>}
              {retentionNotice && <div className="conversation-history-notice" role="status"><AlertTriangle size={14} /><span><strong>{retentionNotice.title}</strong>{retentionNotice.detail}</span></div>}
              {!eventsLoading && conversation.length === 0 && <div className="empty-conversation"><div className="empty-symbol"><MessageSquare size={22} /></div><strong>{draftSelected ? 'What would you like to work on?' : 'Start a conversation'}</strong><div className="workflow-starters">{workflowStarters.map((workflow) => <button key={workflow.id} type="button" onClick={() => {if (workflow.id === 'deploy-model' && connection?.capabilities?.capabilities.includes('deployments.v1')) {setShowDeployment(true); return;} setComposer(workflow.prompt); window.requestAnimationFrame(() => composerRef.current?.focus());}}>{workflow.title}<ChevronRight size={13} /></button>)}</div></div>}
              {conversation.map((item) => item.kind === 'user'
                ? <UserMessage key={item.key} item={item} onEdit={editMessage} />
                : <AssistantTurn key={item.key} item={item} running={selectedTask.status === 'running' && !optimisticPrompt && item === conversation[conversation.length - 1]} canCheckModel={Boolean(effectiveModel?.provider === 'drobotics' && connection?.capabilities?.capabilities.includes('models.health.v1'))} checkingModel={checkingModel} onCheckModel={() => void checkModelHealth()} onRetry={() => retryFailedTurn(item)} />)}
              {optimisticPrompt?.taskId === selectedTask.id && !events.some((entry) => entry.normalized?.type === 'user.message' && String(entry.normalized.data?.text ?? '') === optimisticPrompt.text) && <UserMessage item={{kind: 'user', key: 'optimistic', sequence: Number.MAX_SAFE_INTEGER, time: optimisticPrompt.time, text: optimisticPrompt.text, attachments: optimisticPrompt.attachments.map((image) => ({name: image.name, mimeType: image.mimeType, preview: imageDataURL(image)})), source: 'user', scheduleId: ''}} />}
              {selectedTask.status === 'queued' ? <div className="agent-progress immediate"><ListTodo size={14} /><span>Waiting for a board Agent slot</span><small>{selectedTask.queuedAt ? relativeTime(selectedTask.queuedAt) : 'queued'}</small></div> : ['starting', 'running'].includes(selectedTask.status) && <AgentProgress startedAt={activityStart} now={activityClock} hasOutput={!optimisticPrompt && latestConversationItem?.kind === 'assistant' && Boolean(latestConversationItem.text || latestConversationItem.thinking || latestConversationItem.tools.length)} />}
              {selectedTaskRecovery && <TaskRecoveryCard presentation={selectedTaskRecovery} canCheckModel={Boolean(effectiveModel?.provider === 'drobotics' && connection?.capabilities?.capabilities.includes('models.health.v1'))} canDiagnose={Boolean(connection?.capabilities?.capabilities.includes('support.bundle.v1'))} busy={busy || checkingModel} onAction={() => {if (selectedTaskRecovery.recovery === 'check-model') {void checkModelHealth(); return;} if (selectedTaskRecovery.recovery === 'diagnose') {void saveSupportBundle(); return;} setComposer(selectedTaskRecovery.action?.prompt ?? ''); window.requestAnimationFrame(() => composerRef.current?.focus());}} />}
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
                <label className="model-picker" title={canChangeModel ? 'Choose model' : 'Stop the current turn before changing models'}><select aria-label="Model" value={modelPickerValue} disabled={!canChangeModel} onChange={(event) => void changeModel(event.target.value)}><option value="" disabled>Board default</option>{selectedModel && !models.some((model) => `${model.provider}/${model.id}` === selectedModel) && <option value={selectedModel}>{selectedModel}</option>}{models.map((model) => <option key={`${model.provider}/${model.id}`} value={`${model.provider}/${model.id}`}>{model.provider === 'drobotics' ? model.name || model.id : `${model.provider} · ${model.name || model.id}`}</option>)}</select><ChevronDown size={12} /></label>
                {connection?.capabilities?.capabilities.some((capability) => ['models.health.v1', 'models.conformance.v1', 'models.runtime-probe.v1', 'models.rdk-probe.v1'].includes(capability)) && <button className={`model-health-button model-readiness-button compact ${modelReadiness.tone}`} type="button" title={`${modelReadiness.label}: ${modelReadiness.title}`} aria-label={`Model readiness: ${modelReadiness.label}`} onClick={() => setShowModelReadiness(true)} disabled={!effectiveModel}>{checkingModel || verifyingModel || modelQualificationStage ? <LoaderCircle size={12} className="spin" /> : modelReadiness.tone === 'failed' ? <AlertTriangle size={12} /> : <ShieldCheck size={12} />}</button>}
				<button className={`access-mode-button ${accessPresentation.tone}`} type="button" title={`${draftSelected ? `${selectedTask.workspaceMode === 'worktree' ? 'Isolated workspace' : 'Shared workspace'} · ` : ''}${accessPresentation.summary}`} aria-label="Task settings" onClick={() => setShowAccessSettings(true)}><ShieldCheck size={13} /><span>{draftSelected ? 'Task settings' : accessPresentation.label}</span><ChevronDown size={12} /></button>
				<span className="composer-state">{connectionState !== 'online' ? 'Offline · draft preserved' : workspaceInspectionLoading ? 'Checking workspace' : draftSelected || awaitingFirstPrompt ? 'Starts when sent' : editingMessage !== null ? 'Replaces later messages' : selectedComposerMode === 'resume' ? 'Resume session' : selectedComposerMode === 'restart' ? 'New session' : statusLabel[selectedTask.status] ?? selectedTask.status}</span>
                {connectionState !== 'online' ? <button className="send-button reconnect-mode" type="button" title="Reconnect" onClick={() => void refreshWorkspace()} disabled={refreshing}><RefreshCw size={15} className={refreshing ? 'spin' : ''} /></button> : canCancelCurrentWork ? <button className="send-button stop-mode" type="button" title={cancellationMode === 'stop' ? 'Cancel queued task (Esc)' : 'Stop current turn (Esc)'} aria-label={cancellationMode === 'stop' ? 'Cancel queued task' : 'Stop current turn'} onClick={cancelCurrentWork} disabled={busy}><Square size={14} fill="currentColor" /></button> : <button className="send-button" type="submit" title="Send" disabled={!composer.trim() || composerBlocked}><ArrowUp size={17} /></button>}
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
      {showAbout && <AboutDialog appVersion={appVersion} connection={connection} onInstall={async () => {if (!connection) throw new Error('Connect a board before updating.'); const result = await api.installBoardUpdate(connection.board.id); await connect(result.connection.board); return result;}} onClose={() => setShowAbout(false)} />}
      {showExtensions && connection && <ExtensionCenterDialog boardId={connection.board.id} boardName={connection.board.name} boardTarget={snapshot?.boardId || connection.compatibility?.boardId || ''} taskId={selectedTask?.id.startsWith('draft:') ? '' : selectedTask?.id ?? ''} taskName={selectedTask?.name ?? ''} onClose={() => setShowExtensions(false)} />}
		{showSchedules && connection && <ScheduleDialog boardId={connection.board.id} tasks={tasks.filter((task) => !task.branchKind && !task.id.startsWith('draft:'))} selectedTaskId={selectedTask?.branchKind ? '' : selectedTask?.id} onClose={() => setShowSchedules(false)} />}
      {showProviders && connection && <ProviderDialog boardId={connection.board.id} boardName={connection.board.name} models={models} onChanged={async (result) => {setNotice(result.message); if (result.applied) {const nextModels = await api.models(connection.board.id); setModels(nextModels);}}} onClose={() => setShowProviders(false)} />}
      {supportBundle && <SupportDiagnosticsDialog bundle={supportBundle} onClose={() => setSupportBundle(null)} />}
      {showDiagnostics && <ReadinessDiagnosticsDialog report={diagnostics} loading={diagnosticsLoading} failure={diagnosticsError} onRefresh={() => void openDiagnostics()} onRepair={(action) => void repairDiagnostic(action)} onClose={() => setShowDiagnostics(false)} />}
      {showBPUBenchmark && connection && <BPUBenchmarkDialog boardId={connection.board.id} boardName={connection.board.name} snapshot={snapshot} cwd={selectedTask?.cwd ?? '/root'} onClose={() => setShowBPUBenchmark(false)} />}
      {showModelReadiness && connection && effectiveModel && <ModelReadinessDialog model={effectiveModel} snapshot={snapshot} capabilities={connection.capabilities?.capabilities ?? []} qualification={qualificationForModel} health={currentModelHealth} conformance={currentModelConformance} runtimeProbe={currentModelRuntimeProbe} rdkProbe={currentModelRDKProbe} rdkMatrix={currentModelRDKMatrix} activeStage={checkingModel ? 'health' : verifyingModel ? 'protocol' : modelQualificationStage} activeProfile={modelQualificationProfile} failure={modelReadinessError} onRunHealth={() => void checkModelHealth()} onRunProtocol={() => void verifyModelConformance()} onRunRuntime={() => void probeModelRuntime()} onRunRDK={(profile) => void probeModelRDK(profile)} onClose={() => setShowModelReadiness(false)} />}

      {showAccessSettings && selectedTask && <AccessSettingsDialog permissionMode={selectedPermissionMode} sandboxMode={selectedSandboxMode} networkMode={selectedNetworkMode} workspaceMode={selectedTask.workspaceMode || 'shared'} summary={accessPresentation.summary} hasWorkspace={Boolean(draftSelected && connection?.capabilities?.capabilities.includes('workspaces.isolation.v1'))} workspaceEligible={Boolean(workspaceInspection?.result?.eligible)} workspaceLoading={workspaceInspectionLoading} workspaceReason={workspaceInspection?.result?.reason || ''} hasSandbox={Boolean(selectedTask.sandboxMode || connection?.capabilities?.capabilities.includes('tasks.sandbox.v1'))} hasNetwork={Boolean(selectedTask.networkMode || connection?.capabilities?.capabilities.includes('tasks.network.v1'))} sandboxAvailable={Boolean(connection?.capabilities?.sandbox?.available)} networkModes={connection?.capabilities?.sandbox?.networkModes ?? []} modelOnly={effectiveModel?.modelOnly === true} canChangePermissions={canChangePermissions} canChangeSandbox={canChangeSandbox} canChangeNetwork={canChangeNetwork} canStopWorker={canStopBoundaryWorker} busy={busy} onWorkspace={changeWorkspaceMode} onPermission={(mode) => void changePermissionMode(mode)} onSandbox={(mode) => void changeSandboxMode(mode)} onNetwork={(mode) => void changeNetworkMode(mode)} onStopWorker={() => void stopTask()} onClose={() => setShowAccessSettings(false)} />}
      {showDeployment && selectedTask && snapshot && <DeploymentDialog boardId={boardId} cwd={selectedTask.cwd} snapshot={snapshot} models={models} busy={busy} onClose={() => setShowDeployment(false)} onStart={startDeployment} />}
	      {showChanges && selectedTask && boardId && <WorkspaceChangesDialog boardId={boardId} task={selectedTask} canDeliver={Boolean(connection?.capabilities?.capabilities.includes('workspaces.delivery.v1'))} onClose={() => setShowChanges(false)} />}
      {deleteTarget && <DeleteDialog target={deleteTarget} busy={busy} onClose={() => setDeleteTarget(null)} onDelete={confirmDelete} />}
    </div>
  );
}

function UserMessage({item, onEdit}: {item: UserConversationItem; onEdit?: (item: UserConversationItem) => void}) {
  const scheduled = item.source === 'schedule';
  return <article className={`user-message${scheduled ? ' scheduled-message' : ''}`}>{scheduled && <div className="scheduled-message-label" title={item.scheduleId ? `Schedule ${item.scheduleId}` : 'Scheduled task'}><CalendarClock size={13} />Scheduled</div>}{item.attachments.length > 0 && <div className="message-attachments">{item.attachments.map((attachment, index) => attachment.preview ? <img key={`${attachment.name}-${index}`} src={attachment.preview} alt={attachment.name || `Attached image ${index + 1}`} /> : <span key={`${attachment.name}-${index}`}><Paperclip size={12} />{attachment.name || attachment.mimeType}</span>)}</div>}<div className="user-message-content">{item.text}</div><div className="message-actions"><time>{formatTime(item.time)}</time><CopyButton value={item.text} />{onEdit && !scheduled && <button className="copy-button" title="Edit from this point" onClick={() => onEdit(item)}><FilePenLine size={14} /></button>}</div></article>;
}

function AssistantTurn({item, running, canCheckModel, checkingModel, onCheckModel, onRetry}: {item: AssistantConversationItem; running: boolean; canCheckModel: boolean; checkingModel: boolean; onCheckModel: () => void; onRetry: () => void}) {
  return <article className="assistant-turn">
    {item.thinking && <ThinkingBlock item={item} running={running} />}
    {item.tools.length > 0 && <ToolGroup tools={item.tools} />}
    {item.notices.map((notice, index) => <div key={`${notice.time}-${index}`} className={`turn-notice notice-${notice.type}`}><Activity size={13} /><span>{notice.label}</span></div>)}
    {item.text && <div className="assistant-content"><MarkdownContent value={item.text} /><div className="assistant-actions"><CopyButton value={item.text} /></div></div>}
    {item.failure && <section className="turn-failure" role="alert"><div className="turn-failure-heading"><AlertTriangle size={16} /><strong>{item.failure.title}</strong></div><p>{item.failure.message}</p><div className="turn-failure-actions">{canCheckModel && <button className="secondary-button" type="button" onClick={onCheckModel} disabled={checkingModel}>{checkingModel ? <LoaderCircle size={14} className="spin" /> : <Activity size={14} />}Check model</button>}<button className="secondary-button" type="button" onClick={onRetry}><RefreshCw size={14} />Edit and retry</button></div></section>}
	{running && !item.failure && <div className="agent-progress"><LoaderCircle size={14} className="spin" /><span>{item.retry?.active ? `Automatic retry ${item.retry.attempt}/${item.retry.maxAttempts}` : 'Working'}</span></div>}
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
  return <div className="markdown"><ReactMarkdown skipHtml remarkPlugins={markdownRemarkPlugins} rehypePlugins={markdownRehypePlugins} components={{
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

function AccessSettingsDialog({permissionMode, sandboxMode, networkMode, workspaceMode, summary, hasWorkspace, workspaceEligible, workspaceLoading, workspaceReason, hasSandbox, hasNetwork, sandboxAvailable, networkModes, modelOnly, canChangePermissions, canChangeSandbox, canChangeNetwork, canStopWorker, busy, onWorkspace, onPermission, onSandbox, onNetwork, onStopWorker, onClose}: {
  permissionMode: string;
  sandboxMode: string;
  networkMode: string;
  workspaceMode: string;
  summary: string;
  hasWorkspace: boolean;
  workspaceEligible: boolean;
  workspaceLoading: boolean;
  workspaceReason: string;
  hasSandbox: boolean;
  hasNetwork: boolean;
  sandboxAvailable: boolean;
  networkModes: string[];
  modelOnly: boolean;
  canChangePermissions: boolean;
  canChangeSandbox: boolean;
  canChangeNetwork: boolean;
  canStopWorker: boolean;
  busy: boolean;
  onWorkspace: (mode: string) => void;
  onPermission: (mode: string) => void;
  onSandbox: (mode: string) => void;
  onNetwork: (mode: string) => void;
  onStopWorker: () => void;
  onClose: () => void;
}) {
  const restrictedNetwork = sandboxMode !== 'off';
  const boundariesLocked = (hasSandbox && !canChangeSandbox) || (hasNetwork && !canChangeNetwork);
  return <div className="modal-backdrop"><section className="modal access-settings-modal" role="dialog" aria-modal="true" aria-labelledby="access-settings-title">
    <div className="modal-header"><div><span className="modal-eyebrow">Agent defaults</span><h2 id="access-settings-title">Task settings</h2></div><button className="icon-button" title="Close" onClick={onClose}><X size={18} /></button></div>
    <div className="access-settings-summary"><ShieldCheck size={18} /><div><strong>{summary}</strong><span>These boundaries are enforced on the board, not by Studio.</span></div></div>
    <div className="form-grid access-settings-grid">
      {hasWorkspace && <label><span>Workspace</span><select aria-label="Workspace mode" value={workspaceMode} disabled={busy || workspaceLoading} onChange={(event) => onWorkspace(event.target.value)}><option value="shared">Shared project</option><option value="worktree" disabled={!workspaceEligible}>Isolated worktree</option></select><small>{workspaceLoading ? 'Checking whether this project supports isolation.' : workspaceReason || 'An isolated worktree keeps concurrent Agent edits separate.'}</small></label>}
      <label><span>Approvals</span><select aria-label="Approval mode" value={permissionMode} disabled={busy || !canChangePermissions} onChange={(event) => onPermission(event.target.value)}><option value="review">Review only</option><option value="ask">Ask for changes</option><option value="developer">Developer</option></select><small>Controls which tool actions require confirmation.</small></label>
      {hasSandbox && <label><span>Board access</span><select aria-label="OS sandbox" value={sandboxMode} disabled={busy || !canChangeSandbox} onChange={(event) => onSandbox(event.target.value)}><option value="review" disabled={!sandboxAvailable}>Read only</option><option value="workspace" disabled={!sandboxAvailable}>Workspace</option><option value="system" disabled={!sandboxAvailable}>Board hardware</option><option value="off" disabled={networkMode !== 'shared'}>No sandbox</option></select><small>Limits files, devices, and Linux capabilities.</small></label>}
      {hasNetwork && <label><span>Network</span><select aria-label="Network boundary" value={networkMode} disabled={busy || !canChangeNetwork} onChange={(event) => onNetwork(event.target.value)}><option value="shared">Network</option><option value="model-only" disabled={!modelOnly || !restrictedNetwork || !networkModes.includes('model-only')}>Model only</option><option value="offline" disabled={!restrictedNetwork || !networkModes.includes('offline')}>Offline</option></select><small>Separates model access from general tool networking.</small></label>}
    </div>
    {boundariesLocked && <div className="access-settings-note boundary-locked"><Info size={14} /><span>Board access and Network are fixed while this Agent worker exists, including when it is Ready. Stop the Agent to unlock them, then change the settings and Resume. Approvals can still change while Ready.</span>{canStopWorker && <button className="secondary-button" type="button" onClick={onStopWorker} disabled={busy}><Square size={12} fill="currentColor" />Stop Agent</button>}</div>}
    {sandboxMode === 'off' && <div className="access-settings-note danger"><AlertTriangle size={14} /><span>No sandbox gives tools the board user's host access and requires shared networking.</span></div>}
    <div className="modal-actions"><button className="primary-button" type="button" onClick={onClose}>Done</button></div>
  </section></div>;
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

type ReadinessStage = 'health' | 'protocol' | 'runtime' | 'rdk' | '';

function ModelReadinessDialog({model, snapshot, capabilities, qualification, health, conformance, runtimeProbe, rdkProbe, rdkMatrix, activeStage, activeProfile, failure, onRunHealth, onRunProtocol, onRunRuntime, onRunRDK, onClose}: {
  model: ModelOption;
  snapshot: SystemSnapshot | null;
  capabilities: string[];
  qualification?: ModelQualification;
  health?: ModelHealth;
  conformance?: ModelConformance;
  runtimeProbe?: ModelRuntimeProbe;
  rdkProbe?: ModelRDKProbe;
  rdkMatrix?: ModelRDKMatrix;
  activeStage: ReadinessStage;
  activeProfile: string;
  failure: string;
  onRunHealth: () => void;
  onRunProtocol: () => void;
  onRunRuntime: () => void;
  onRunRDK: (profile: string) => void;
  onClose: () => void;
}) {
  const has = (capability: string) => capabilities.includes(capability);
  const busy = Boolean(activeStage);
	const summary = modelReadinessPresentation({health, conformance, runtime: runtimeProbe, rdk: rdkProbe, evidenceState: qualification?.state, running: busy});
	const evidenceNotice = qualificationEvidenceNotice(qualification);
	const directGatewayApplicable = model.provider === 'drobotics';
  const healthState = !has('models.health.v1') || !directGatewayApplicable ? 'unsupported' : activeStage === 'health' ? 'running' : health?.status === 'available' ? 'passed' : health?.status === 'unavailable' ? 'failed' : 'idle';
  const protocolState = !has('models.conformance.v1') || !directGatewayApplicable ? 'unsupported' : activeStage === 'protocol' ? 'running' : conformance?.status === 'failed' ? 'failed' : conformance?.status === 'compatible' ? 'partial' : conformance ? 'passed' : 'idle';
  const runtimeState = !has('models.runtime-probe.v1') ? 'unsupported' : activeStage === 'runtime' ? 'running' : runtimeProbe?.status === 'failed' ? 'failed' : runtimeProbe ? 'partial' : 'idle';
	const rdkApplicable = (model.provider === 'drobotics' || model.managed === true) && ['x5', 's100', 's600'].includes(snapshot?.boardId ?? '');
  const rdkState = !has('models.rdk-probe.v1') || !rdkApplicable ? 'unsupported' : activeStage === 'rdk' ? 'running' : rdkProbe?.status === 'failed' ? 'failed' : rdkProbe?.status === 'passed' && rdkProbe.releaseEligible ? 'passed' : rdkProbe ? 'partial' : 'idle';
  const matrixReady = has('models.rdk-matrix.v1') && Boolean(rdkMatrix);
  const runLabel = (stage: ReadinessStage, result?: unknown) => activeStage === stage ? 'Running' : result ? 'Run again' : 'Run';
  return <div className="modal-backdrop"><section className="modal model-readiness-modal" role="dialog" aria-modal="true" aria-labelledby="model-readiness-title">
    <div className="modal-header"><div><span className="modal-eyebrow">{model.provider}</span><h2 id="model-readiness-title">{model.name || model.id} readiness</h2></div><button className="icon-button" title="Close" onClick={onClose}><X size={18} /></button></div>
    <div className="readiness-overview"><div className={`readiness-summary ${summary.tone}`}><ReadinessStateIcon state={summary.tone} /><span><strong>{summary.label}</strong><small>{summary.title}</small></span></div>{evidenceNotice && <div className="readiness-evidence-notice"><RefreshCw size={13} /><span>{evidenceNotice}</span></div>}{failure && <div className="readiness-failure" role="alert"><AlertTriangle size={14} /><span>{failure}</span></div>}</div>
    <div className="readiness-layers">
      <ReadinessLayer index="01" title="Route" subtitle="Minimal model request" state={healthState} detail={health ? `${health.message}${health.latencyMs ? ` ${health.latencyMs} ms.` : ''}${health.cached ? ' Cached result.' : ''}` : directGatewayApplicable ? 'Checks credentials, routing, and basic availability only.' : 'Direct route probing is currently available for the built-in D-Robotics gateway. Use Agent runtime for this managed provider.'} duration="Usually under 20 seconds" action={has('models.health.v1') && directGatewayApplicable ? <button className="secondary-button" disabled={busy} onClick={onRunHealth}>{activeStage === 'health' && <LoaderCircle size={13} className="spin" />}{runLabel('health', health)}</button> : undefined} />
      <ReadinessLayer index="02" title="Gateway protocol" subtitle="Streaming, tools, continuation, inputs" state={protocolState} detail={conformance?.message || (directGatewayApplicable ? 'Tests the gateway contract without running a real Agent or RDK task.' : 'Direct protocol qualification is provider-specific. The isolated Agent runtime test covers the actual Pi integration instead.')} duration="Usually under 1 minute" action={has('models.conformance.v1') && directGatewayApplicable ? <button className="secondary-button" disabled={busy} onClick={onRunProtocol}>{activeStage === 'protocol' && <LoaderCircle size={13} className="spin" />}{runLabel('protocol', conformance)}</button> : undefined} />
      <ReadinessLayer index="03" title="Agent runtime" subtitle="Tools, approvals, thinking, compaction, recovery" state={runtimeState} detail={runtimeProbe?.message || 'Runs an isolated Agent suite with synthetic tools and a forced interrupted-session recovery.'} duration="Can take up to 15 minutes" extra={runtimeProbe?.pending?.length ? <span>Still pending: {runtimeProbe.pending.join(', ')}</span> : undefined} action={has('models.runtime-probe.v1') ? <button className="secondary-button" disabled={busy} onClick={onRunRuntime}>{activeStage === 'runtime' && <LoaderCircle size={13} className="spin" />}{runLabel('runtime', runtimeProbe)}</button> : undefined} />
      <ReadinessLayer index="04" title="RDK workflows" subtitle={snapshot ? `${snapshot.boardId.toUpperCase()} · RDK OS ${snapshot.rdkOsVersion}` : 'Recognized RDK board required'} state={rdkState} detail={matrixReady ? 'Evidence is recorded independently for each bounded board workflow.' : rdkProbe?.message || 'Runs one named, read-only board diagnostic profile against live board state and versioned official knowledge.'} duration="Each runnable profile can take up to 5 minutes" extra={matrixReady ? <RDKProfileRows profiles={rdkMatrix!.profiles} activeProfile={activeProfile} busy={busy} onRun={onRunRDK} /> : rdkProbe ? <><span>Profile: {rdkProbe.profile}</span><span>Knowledge: {rdkProbe.binding.knowledgeVersion} · Build: {rdkProbe.binding.buildStatus}{rdkProbe.binding.dirty ? ' (dirty)' : ''}</span><span>Not covered: {rdkProbe.notCovered.join(', ')}</span>{rdkProbe.sources?.map((source) => <button key={source} className="readiness-source" onClick={() => void api.openExternalURL(source)}>{source}</button>)}</> : !rdkApplicable ? <span>Available for built-in or explicitly managed models on a detected X5, S100, or S600.</span> : undefined} action={!matrixReady && has('models.rdk-probe.v1') && rdkApplicable ? <button className="secondary-button" disabled={busy} onClick={() => onRunRDK('read-only-rdk-diagnostic-v1')}>{activeStage === 'rdk' && <LoaderCircle size={13} className="spin" />}{runLabel('rdk', rdkProbe)}</button> : undefined} />
    </div>
    <div className="readiness-footer"><Info size={14} /><span>Opening this panel reads private board evidence without calling a model. Only Run actions make model requests; results stay bound to this exact model, board, build, and named scope.</span><button className="primary-button" onClick={onClose}>Done</button></div>
  </section></div>;
}

function ReadinessLayer({index, title, subtitle, state, detail, duration, extra, action}: {index: string; title: string; subtitle: string; state: string; detail: string; duration: string; extra?: ReactNode; action?: ReactNode}) {
	return <section className={`readiness-layer ${state}`}><span className="readiness-index">{index}</span><ReadinessStateIcon state={state} /><div className="readiness-layer-copy"><div><strong>{title}</strong><small>{subtitle}</small></div><p>{detail}</p>{extra && <div className="readiness-extra">{extra}</div>}<span className="readiness-duration">{state === 'unsupported' ? 'Not available for this model or connection' : duration}</span></div><div className="readiness-action">{action}</div></section>;
}

function RDKProfileRows({profiles, activeProfile, busy, onRun}: {profiles: ModelRDKProfileStatus[]; activeProfile: string; busy: boolean; onRun: (profile: string) => void}) {
	return <div className="rdk-profile-list">{profiles.map((profile) => {
		const state = rdkProfileState(profile, activeProfile);
		const runnable = profile.availability === 'available';
		return <div className={`rdk-profile-row ${state}`} key={profile.id}>
			<ReadinessStateIcon state={state} />
			<div className="rdk-profile-copy"><div><strong>{profile.name}</strong><small>{profile.evidenceClass} · {rdkProfileEvidenceLabel(profile)}</small></div><p>{profile.description}</p>{profile.evidenceState === 'stale' && profile.staleReasons.length > 0 && <span>Changed: {profile.staleReasons.join(', ')}</span>}{profile.result && <><span>Knowledge: {profile.result.binding.knowledgeVersion} · Build: {profile.result.binding.buildStatus}</span>{profile.result.sources?.map((source) => <button key={source} className="readiness-source" onClick={() => void api.openExternalURL(source)}>{source}</button>)}</>}<span>Not covered: {profile.notCovered.join(', ')}</span></div>
			<button className="secondary-button" disabled={busy || !runnable} onClick={() => onRun(profile.id)}>{state === 'running' && <LoaderCircle size={13} className="spin" />}{profile.availability === 'planned' ? 'Planned' : profile.availability === 'unsupported-target' ? 'Unavailable' : profile.result ? 'Run again' : 'Run'}</button>
		</div>;
	})}</div>;
}

function ReadinessStateIcon({state}: {state: string}) {
  if (state === 'running') return <LoaderCircle className="readiness-state spin" size={17} />;
  if (state === 'failed') return <XCircle className="readiness-state" size={17} />;
  if (state === 'passed') return <Check className="readiness-state" size={17} />;
  if (state === 'partial') return <ShieldCheck className="readiness-state" size={17} />;
  return <Activity className="readiness-state" size={17} />;
}

function BoardDialog({boards, busy, onClose, onConnect, onSave, onRemove}: {boards: Board[]; busy: boolean; onClose: () => void; onConnect: (board: Board) => void; onSave: (board: Board) => Promise<void>; onRemove: (board: Board) => Promise<void>}) {
  const [editing, setEditing] = useState(boards.length === 0);
  const [form, setForm] = useState<Board>({id: '', name: 'RDK S100', host: '', user: 'root', port: 22, identityFile: ''});
  const [working, setWorking] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [connectingId, setConnectingId] = useState<string>('');
  const [probe, setProbe] = useState<Connection | null>(null);
  const [failure, setFailure] = useState('');
  const [removing, setRemoving] = useState<Board | null>(null);
  const [activeBoardTarget, setActiveBoardTarget] = useState<Board | null>(null);

  const beginEdit = (board?: Board) => {
    setForm(board ?? {id: '', name: 'RDK S100', host: '', user: 'root', port: 22, identityFile: ''});
    setProbe(null);
    setFailure('');
    setActiveBoardTarget(null);
    setEditing(true);
  };

  const handleSavedBoardClick = async (board: Board) => {
    setConnectingId(board.id);
    setActiveBoardTarget(board);
    setFailure('');
    setProbe(null);
    try {
      const result = await api.probeBoard(board);
      setProbe(result);
      if (!result.connected) {
        setFailure(result.error || result.compatibility?.summary || 'Could not connect to this board.');
        return;
      }
      onConnect(board);
    } catch (reason) {
      const err = String(reason);
      setFailure(friendlyError(err));
      if (/not installed|command not found|hobot: not found|without a response/i.test(err)) {
        setProbe({
          board,
          connected: false,
          notInstalled: true,
          error: `Hobot Code is not installed on ${board.name}. You can install it automatically.`,
        });
      }
    } finally {
      setConnectingId('');
    }
  };

  const submit = async () => {
    setWorking(true); setFailure(''); setProbe(null); setActiveBoardTarget(form);
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

  const installService = async (boardTarget?: Board) => {
    const target = boardTarget || (editing ? form : activeBoardTarget);
    if (!target) return;
    setInstalling(true); setFailure('');
    try {
      const result = await api.installBoardService(target);
      if (result.success && result.connection.connected) {
        setProbe(result.connection);
        const detected = result.connection.snapshot?.boardId?.toUpperCase();
        const candidate = detected && /^(RDK (S100|S600|X5))$/i.test(target.name) ? {...target, name: `RDK ${detected}`} : target;
        await onSave(candidate);
      } else {
        setFailure(result.message || 'Installation completed, but could not connect to board.');
      }
    } catch (reason) {
      setFailure(friendlyError(String(reason)));
    } finally {
      setInstalling(false);
    }
  };

  const remove = async () => {if (!removing) return; setWorking(true); setFailure(''); try {await onRemove(removing); setRemoving(null); if (boards.length <= 1) beginEdit();} catch (reason) {setFailure(friendlyError(String(reason)));} finally {setWorking(false);}};
  const disabled = busy || working || installing || Boolean(connectingId);

  return (
    <div className="modal-backdrop">
      <div className="modal board-modal">
        <div className="modal-header">
          <div>
            <span className="modal-eyebrow">Boards</span>
            <h2>{editing ? (form.id ? 'Edit board' : 'Add board') : 'Connect'}</h2>
          </div>
          {boards.length > 0 && <button className="icon-button" title="Close" onClick={onClose}><X size={18} /></button>}
        </div>

        {!editing ? (
          <>
            <div className="saved-boards">
              {boards.map((board) => {
                const isConnectingThis = connectingId === board.id;
                const isTarget = activeBoardTarget?.id === board.id;
                return (
                  <div key={board.id} className="saved-board-group">
                    <div className="saved-board-row">
                      <button className="saved-board" onClick={() => void handleSavedBoardClick(board)} disabled={disabled}>
                        {isConnectingThis ? <LoaderCircle size={19} className="spin" /> : <Server size={19} />}
                        <span>
                          <strong>{board.name}</strong>
                          <small>{board.user}@{board.host}:{board.port}</small>
                        </span>
                        <ChevronRight size={15} />
                      </button>
                      <button className="icon-button compact" title={`Edit ${board.name}`} onClick={() => beginEdit(board)} disabled={disabled}>
                        <FilePenLine size={14} />
                      </button>
                      <button className="icon-button compact danger-icon" title={`Remove ${board.name}`} onClick={() => setRemoving(board)} disabled={disabled}>
                        <Trash2 size={14} />
                      </button>
                    </div>
                    {isTarget && failure && (
                      <ConnectionFailure
                        result={probe}
                        message={failure}
                        onInstall={() => void installService(board)}
                        installing={installing}
                      />
                    )}
                  </div>
                );
              })}
            </div>
            <button className="add-board-row" onClick={() => beginEdit()} disabled={disabled}>
              <Plus size={16} />Add board
            </button>
          </>
        ) : (
          <form onSubmit={(event) => {event.preventDefault(); void submit();}} className="form-grid">
            <div className="board-presets">
              {boardPresets.map((preset) => (
                <button type="button" key={preset.name} className={form.name === preset.name ? 'selected' : ''} onClick={() => setForm({...form, ...preset})}>
                  <Server size={14} /><span>{preset.name}</span>
                </button>
              ))}
            </div>
            <label><span>Name</span><input value={form.name} onChange={(event) => setForm({...form, name: event.target.value})} required /></label>
            <label><span>Host</span><input value={form.host} onChange={(event) => setForm({...form, host: event.target.value})} placeholder="Board IP or hostname" autoFocus required /></label>
            <div className="form-row">
              <label><span>User</span><input value={form.user} onChange={(event) => setForm({...form, user: event.target.value})} required /></label>
              <label><span>Port</span><input type="number" min="1" max="65535" value={form.port} onChange={(event) => setForm({...form, port: Number(event.target.value)})} required /></label>
            </div>
            <label><span>Identity file</span><input value={form.identityFile} onChange={(event) => setForm({...form, identityFile: event.target.value})} placeholder="Use SSH agent or config" /></label>
            {failure && <ConnectionFailure result={probe} message={failure} onInstall={() => void installService(form)} installing={installing} />}
            {probe?.connected && probe.snapshot && <div className="probe-success"><Check size={14} /><span>Detected {probe.snapshot.board} · RDK OS {probe.snapshot.rdkOsVersion}</span></div>}
            <div className="modal-actions">
              {boards.length > 0 && <button type="button" className="secondary-button" onClick={() => {setEditing(false); setFailure(''); setProbe(null); setActiveBoardTarget(null);}}>Back</button>}
              <button className="primary-button" type="submit" disabled={disabled}>
                {disabled ? <LoaderCircle size={15} className="spin" /> : <Server size={15} />}
                Verify, save & connect
              </button>
            </div>
          </form>
        )}

        {removing && (
          <div className="inline-confirm">
            <AlertTriangle size={16} />
            <span><strong>Remove {removing.name}?</strong><small>Board tasks keep running; only this saved connection is removed.</small></span>
            <button className="secondary-button" onClick={() => setRemoving(null)} disabled={disabled}>Cancel</button>
            <button className="danger-button" onClick={() => void remove()} disabled={disabled}>Remove</button>
          </div>
        )}
      </div>
    </div>
  );
}


function ConnectionFailure({result, message, onInstall, installing}: {result: Connection | null; message: string; onInstall?: () => void; installing?: boolean}) {
  const isNotInstalled = Boolean(result?.notInstalled || /not installed|command not found|hobot: not found/i.test(message));
  return (
    <div className="connection-failure" role="alert">
      <AlertTriangle size={15} />
      <span>
        <strong>{message}</strong>
        {result?.compatibility?.issues.map((issue) => <small key={issue.code}>{issue.action || issue.message}</small>)}
        {!isNotInstalled && <small>Check the VPN, SSH access, and that `hobot daemon start` is running.</small>}
        {isNotInstalled && onInstall && (
          <div className="install-prompt-row">
            <small>You can automatically install Hobot Code on this board over SSH.</small>
            <button
              type="button"
              className="primary-button compact install-service-button"
              onClick={onInstall}
              disabled={installing}
            >
              {installing ? <LoaderCircle size={13} className="spin" /> : <Download size={13} />}
              <span>{installing ? 'Installing on board...' : 'Install on board'}</span>
            </button>
          </div>
        )}
      </span>
    </div>
  );
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
  const [studioCheck, setStudioCheck] = useState<StudioUpdateCheck | null>(null);
  const [boardCheck, setBoardCheck] = useState<BoardUpdateCheck | null>(null);
  const [checkingStudio, setCheckingStudio] = useState(false);
  const [checkingBoard, setCheckingBoard] = useState(false);
  const [openingStudio, setOpeningStudio] = useState(false);
  const [installingBoard, setInstallingBoard] = useState(false);
  const [studioFailure, setStudioFailure] = useState('');
  const [boardFailure, setBoardFailure] = useState('');
  const [studioSuccess, setStudioSuccess] = useState('');
  const [boardSuccess, setBoardSuccess] = useState('');
  const boardID = connection?.board.id ?? '';
  const activeTasks = connection?.daemon?.activeTasks ?? 0;

  const checkStudio = useCallback(async () => {
    setCheckingStudio(true);
    setStudioFailure('');
    try {
      setStudioCheck(await api.checkStudioUpdate());
    } catch (reason) {
      setStudioFailure(friendlyError(String(reason)));
    } finally {
      setCheckingStudio(false);
    }
  }, []);

  const checkBoard = useCallback(async () => {
    if (!boardID) return;
    setCheckingBoard(true);
    setBoardFailure('');
    try {
      setBoardCheck(await api.checkBoardUpdate(boardID));
    } catch (reason) {
      setBoardFailure(friendlyError(String(reason)));
    } finally {
      setCheckingBoard(false);
    }
  }, [boardID]);

  useEffect(() => {
    void checkStudio();
  }, [appVersion]);

  useEffect(() => {
    setBoardCheck(null);
    setBoardSuccess('');
    if (boardID) void checkBoard();
  }, [boardID, connection?.daemon?.version]);

  const openStudioUpdate = async () => {
    if (openingStudio || studioCheck?.status !== 'available') return;
    setOpeningStudio(true);
    setStudioFailure('');
    setStudioSuccess('');
    try {
      await api.openStudioUpdate();
      setStudioSuccess('The signed macOS DMG download started. Board tasks continue running.');
    } catch (reason) {
      setStudioFailure(friendlyError(String(reason)));
    } finally {
      setOpeningStudio(false);
    }
  };

  const installBoard = async (continueWithStudio = false) => {
    if (installingBoard || boardCheck?.status !== 'available' || activeTasks > 0) return;
    setInstallingBoard(true);
    setBoardFailure('');
    setBoardSuccess('');
    try {
      const result = await onInstall();
      setBoardCheck({status: 'current', installedVersion: result.installedVersion, availableVersion: result.installedVersion, message: 'This board is up to date.'});
      setBoardSuccess(result.message);
      if (continueWithStudio && studioCheck?.status === 'available') await openStudioUpdate();
    } catch (reason) {
      setBoardFailure(friendlyError(String(reason)));
    } finally {
      setInstallingBoard(false);
    }
  };

  const studioTone = studioFailure ? 'failed' : studioCheck?.status === 'available' ? 'available' : studioCheck?.status === 'ahead' ? 'ahead' : studioCheck ? 'current' : 'checking';
  const boardTone = boardFailure ? 'failed' : boardCheck?.status === 'available' ? 'available' : boardCheck ? 'current' : 'checking';
  const bothAvailable = studioCheck?.status === 'available' && boardCheck?.status === 'available';
  const busy = checkingStudio || checkingBoard || openingStudio || installingBoard;

  return <div className="modal-backdrop"><div className="modal about-modal"><div className="modal-header"><div><span className="modal-eyebrow">Hobot Code</span><h2>Version & updates</h2></div><button className="icon-button" title="Close" onClick={onClose} disabled={installingBoard}><X size={18} /></button></div><div className="about-content">
    <div className="about-mark">H</div>
    <section className="update-section">
      <div className="update-section-heading"><span><strong>Studio for Mac</strong><small>Installed v{appVersion || 'unknown'}</small></span>{studioCheck?.availableVersion && <code>v{studioCheck.availableVersion}</code>}</div>
      <div className={`board-update-state ${studioTone}`}>{checkingStudio || openingStudio ? <LoaderCircle size={17} className="spin" /> : studioFailure ? <AlertTriangle size={17} /> : studioCheck?.status === 'available' ? <Download size={17} /> : studioCheck?.status === 'ahead' ? <Info size={17} /> : <Check size={17} />}<span><strong>{openingStudio ? 'Opening signed release' : checkingStudio ? 'Checking Studio updates' : studioFailure ? 'Studio update unavailable' : studioCheck?.status === 'available' ? 'A Studio update is ready' : studioCheck?.status === 'ahead' ? 'Newer than the public release' : studioCheck ? 'Studio is up to date' : 'Not checked'}</strong><small>{studioFailure || studioSuccess || studioCheck?.message || 'Checks the official stable release without changing the board.'}</small></span></div>
      <div className="update-actions">{studioCheck?.status === 'available' && <button className="primary-button" disabled={busy} onClick={() => void openStudioUpdate()}>{openingStudio ? <LoaderCircle size={14} className="spin" /> : <Download size={14} />}Download v{studioCheck.availableVersion}</button>}<button className="secondary-button" disabled={busy} onClick={() => void checkStudio()}><RefreshCw size={14} className={checkingStudio ? 'spin' : ''} />Check Studio</button></div>
    </section>
    <section className="update-section">
      <div className="update-section-heading"><span><strong>Board service</strong><small>{connection?.board.name ?? 'No board connected'}{connection?.daemon?.version ? ` · v${connection.daemon.version}` : ''}</small></span>{boardCheck?.availableVersion && <code>v{boardCheck.availableVersion}</code>}</div>
      <div className={`board-update-state ${boardTone}`}>{checkingBoard || installingBoard ? <LoaderCircle size={17} className="spin" /> : boardFailure ? <AlertTriangle size={17} /> : boardCheck?.status === 'available' ? <Download size={17} /> : <Check size={17} />}<span><strong>{installingBoard ? `Installing v${boardCheck?.availableVersion ?? ''}` : checkingBoard ? 'Checking board updates' : boardFailure ? 'Board update unavailable' : boardCheck?.status === 'available' ? 'A board update is ready' : boardCheck?.status === 'source-older' ? 'Installed version is newer' : boardCheck ? 'Board is up to date' : 'Connect a board to check'}</strong><small>{installingBoard ? 'Downloading, verifying, installing, and reconnecting.' : boardFailure || boardSuccess || boardCheck?.message || 'Board updates are checked after a connection is established.'}</small></span></div>
      {activeTasks > 0 && <div className="update-blocked"><Info size={14} /><span>{activeTasks} active board task{activeTasks === 1 ? '' : 's'} must finish or stop before updating. Studio updates do not stop them.</span></div>}
      <div className="update-actions">{boardCheck?.status === 'available' && <button className="primary-button" disabled={busy || activeTasks > 0} onClick={() => void installBoard()}>{installingBoard ? <LoaderCircle size={14} className="spin" /> : <Download size={14} />}Update board to v{boardCheck.availableVersion}</button>}<button className="secondary-button" disabled={busy || !connection} onClick={() => void checkBoard()}><RefreshCw size={14} className={checkingBoard ? 'spin' : ''} />Check board</button></div>
    </section>
    {bothAvailable && <button className="primary-button update-all-button" disabled={busy || activeTasks > 0} onClick={() => void installBoard(true)}><RefreshCw size={14} />Update board, then Studio</button>}
    <div className="update-guidance"><ShieldCheck size={15} /><span><strong>Updates stay under your control</strong><small>Studio opens a signed and notarized macOS release. Board updates are transactional, preserve conversations, and roll back after a failed install.</small></span></div>
    <details className="update-technical"><summary>Technical details</summary><div><InfoRow label="Board build" value={buildLabel} /><InfoRow label="Pi runtime" value={build?.piVersion ? `v${build.piVersion}` : 'Not reported'} /><InfoRow label="Pi contract" value={build?.piCompatibilitySha256 ? build.piCompatibilitySha256.slice(0, 12) : 'Not reported'} /><InfoRow label="Compatibility" value={connection?.compatibility?.status ?? 'Not checked'} /></div></details>
    <div className="modal-actions"><button className="primary-button" onClick={onClose} disabled={installingBoard}>Done</button></div>
  </div></div></div>;
}

const providerAPIs: Array<{value: ManagedProvider['api']; label: string}> = [
  {value: 'openai-completions', label: 'OpenAI Chat Completions'},
  {value: 'openai-responses', label: 'OpenAI Responses'},
  {value: 'anthropic-messages', label: 'Anthropic Messages'},
  {value: 'google-generative-ai', label: 'Google Generative AI'},
];

function ProviderDialog({boardId, boardName, models, onChanged, onClose}: {boardId: string; boardName: string; models: ModelOption[]; onChanged: (result: ProviderMutationResult) => Promise<void>; onClose: () => void}) {
  const emptyForm: AddManagedProviderRequest = {id: '', name: '', baseUrl: '', api: 'openai-completions', model: '', modelName: '', contextWindow: 128000, maxTokens: 16384, reasoning: false, image: false, authHeader: false};
  const [providers, setProviders] = useState<ManagedProvider[]>([]);
  const [form, setForm] = useState<AddManagedProviderRequest>(emptyForm);
  const [adding, setAdding] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState('');
  const [pendingApply, setPendingApply] = useState(false);
  const [removeTarget, setRemoveTarget] = useState<ManagedProvider | null>(null);
  const [rotateTarget, setRotateTarget] = useState<ManagedProvider | null>(null);
  const [confirmSharedRotation, setConfirmSharedRotation] = useState(false);
  const [hasKey, setHasKey] = useState(false);
  const apiKeyRef = useRef<HTMLInputElement>(null);
  const request = useRef(0);
  const includedProviders = useMemo(() => includedProviderGroups(models, providers), [models, providers]);

  const load = useCallback(async () => {
    const active = ++request.current;
    setLoading(true);
    setFailure('');
    try {
      const result = await api.providers(boardId);
      if (request.current === active) setProviders(result);
    } catch (reason) {
      if (request.current === active) setFailure(friendlyError(String(reason)));
    } finally {
      if (request.current === active) setLoading(false);
    }
  }, [boardId]);

  useEffect(() => {void load(); return () => {request.current += 1; if (apiKeyRef.current) apiKeyRef.current.value = '';};}, [load]);

  const finishMutation = async (result: ProviderMutationResult) => {
    setPendingApply(!result.applied);
    await load();
    await onChanged(result);
  };
  const add = async (event: FormEvent) => {
    event.preventDefault();
    const apiKey = apiKeyRef.current?.value ?? '';
    if (!apiKey) return;
    setBusy(true);
    setFailure('');
    try {
      const result = await api.addProvider(boardId, form, apiKey);
      if (apiKeyRef.current) apiKeyRef.current.value = '';
      setHasKey(false);
      setForm(emptyForm);
      setAdding(false);
      await finishMutation(result);
    } catch (reason) {
      setFailure(friendlyError(String(reason)));
    } finally {
      if (apiKeyRef.current) apiKeyRef.current.value = '';
      setHasKey(false);
      setBusy(false);
    }
  };
  const remove = async () => {
    if (!removeTarget) return;
    setBusy(true);
    setFailure('');
    try {
      const result = await api.removeProvider(boardId, removeTarget.id);
      setRemoveTarget(null);
      await finishMutation(result);
    } catch (reason) {
      setFailure(friendlyError(String(reason)));
    } finally {
      setBusy(false);
    }
  };
  const rotate = async (event: FormEvent) => {
    event.preventDefault();
    const apiKey = apiKeyRef.current?.value ?? '';
    if (!rotateTarget || !apiKey || (rotateTarget.credentialUsers > 1 && !confirmSharedRotation)) return;
    setBusy(true);
    setFailure('');
    try {
      const result = await api.rotateProvider(boardId, rotateTarget.id, apiKey, confirmSharedRotation);
      if (apiKeyRef.current) apiKeyRef.current.value = '';
      setHasKey(false);
      setConfirmSharedRotation(false);
      setRotateTarget(null);
      await finishMutation(result);
    } catch (reason) {
      setFailure(friendlyError(String(reason)));
    } finally {
      if (apiKeyRef.current) apiKeyRef.current.value = '';
      setHasKey(false);
      setBusy(false);
    }
  };
  const apply = async () => {
    setBusy(true);
    setFailure('');
    try {
      const result = await api.applyProviderConfiguration(boardId);
      await finishMutation(result);
    } catch (reason) {
      setFailure(friendlyError(String(reason)));
    } finally {
      setBusy(false);
    }
  };
  const canSubmit = form.id.length > 0 && form.baseUrl.length > 0 && form.model.length > 0 && hasKey && Number(form.contextWindow) >= 1024 && Number(form.maxTokens) >= 128 && Number(form.maxTokens) <= Number(form.contextWindow);

  return <div className="modal-backdrop"><section className="modal provider-modal" role="dialog" aria-modal="true" aria-labelledby="provider-dialog-title"><div className="modal-header"><div><span className="modal-eyebrow">{boardName}</span><h2 id="provider-dialog-title">Model providers</h2></div><div className="modal-header-actions"><button className="icon-button" title="Refresh providers" onClick={() => void load()} disabled={loading || busy}><RefreshCw size={16} className={loading ? 'spin' : ''} /></button><button className="icon-button" title="Close" onClick={onClose} disabled={busy}><X size={18} /></button></div></div>
    {pendingApply && <div className="provider-pending"><AlertTriangle size={16} /><span><strong>Saved on the board</strong><small>Apply when active Agent work is idle.</small></span><button className="secondary-button" onClick={() => void apply()} disabled={busy}>{busy && <LoaderCircle size={13} className="spin" />}Apply</button></div>}
    {!adding && !rotateTarget ? <><div className="provider-list">
      {includedProviders.length > 0 && <><div className="provider-section-label"><span>Included</span><small>Available through this board</small></div>{includedProviders.map((provider) => <article className="provider-row provider-row-included" key={provider.id}><div className="provider-icon"><Bot size={16} /></div><div className="provider-copy"><div><strong>{provider.name}</strong><span className="provider-key-state included">Built in</span></div><small>Hobot Code managed gateway</small><div className="provider-models">{provider.models.map((model) => <span key={model.id}><code>{model.name || model.id}</code><small>{includedModelSummary(model)}</small></span>)}</div></div><div className="provider-actions provider-read-only" title="Built-in providers are updated with Hobot Code"><ShieldCheck size={14} /></div></article>)}</>}
      <div className="provider-section-label"><span>Custom</span><small>Your API-compatible providers</small></div>
      {loading && providers.length === 0 ? <div className="provider-loading-row"><LoaderCircle size={15} className="spin" />Reading custom providers</div> : providers.map((provider) => <article className="provider-row" key={provider.id}><div className="provider-icon"><KeyRound size={16} /></div><div className="provider-copy"><div><strong>{provider.name || provider.id}</strong><span className={`provider-key-state ${provider.credential}`}>{provider.credential === 'ready' ? 'Key ready' : 'Key missing'}</span>{provider.credentialUsers > 1 && <span className="provider-key-state shared">Shared by {provider.credentialUsers}</span>}</div><small>{provider.api}</small><div className="provider-models">{provider.models.map((model) => <span key={model.id}><code>{model.name || model.id}</code><small>{model.reasoning ? 'Reasoning' : 'Standard'}{model.image ? ' · Images' : ''} · {Math.round(model.contextWindow / 1000)}K</small></span>)}</div></div><div className="provider-actions"><button className="icon-button compact" title={`Rotate key for ${provider.name || provider.id}`} onClick={() => {setFailure(''); setRemoveTarget(null); setHasKey(false); setConfirmSharedRotation(false); setRotateTarget(provider);}} disabled={busy}><RefreshCw size={14} /></button><button className="icon-button compact danger-icon" title={`Remove ${provider.name || provider.id}`} onClick={() => {setFailure(''); setRotateTarget(null); setRemoveTarget(provider);}} disabled={busy}><Trash2 size={14} /></button></div></article>)}
      {!loading && providers.length === 0 && <div className="provider-custom-empty">No custom providers configured.</div>}
    </div><footer className="provider-footer"><span><ShieldCheck size={13} />API keys are only needed for custom providers and stay on the board.</span><button className="primary-button" onClick={() => {setFailure(''); setRemoveTarget(null); setRotateTarget(null); setAdding(true);}} disabled={busy}><Plus size={14} />Add provider</button></footer></> : adding ? <form className="form-grid provider-form" onSubmit={(event) => void add(event)}><div className="form-row provider-identity"><label><span>Provider ID</span><input value={form.id} onChange={(event) => setForm({...form, id: event.target.value.toLowerCase()})} pattern="[a-z0-9][a-z0-9._-]{0,63}" placeholder="acme" autoFocus required /></label><label><span>Display name</span><input value={form.name} onChange={(event) => setForm({...form, name: event.target.value})} maxLength={120} placeholder="Acme Gateway" /></label></div><label><span>API base URL</span><input type="url" value={form.baseUrl} onChange={(event) => setForm({...form, baseUrl: event.target.value})} placeholder="https://models.example.com/v1" required /></label><label><span>Protocol</span><select value={form.api} onChange={(event) => setForm({...form, api: event.target.value as ManagedProvider['api']})}>{providerAPIs.map((option) => <option value={option.value} key={option.value}>{option.label}</option>)}</select></label><div className="form-row provider-identity"><label><span>Model ID</span><input value={form.model} onChange={(event) => setForm({...form, model: event.target.value})} placeholder="coder-v2" required /></label><label><span>Model name</span><input value={form.modelName} onChange={(event) => setForm({...form, modelName: event.target.value})} maxLength={120} placeholder="Acme Coder" /></label></div><div className="form-row provider-limits"><label><span>Context window</span><input type="number" min="1024" max="4000000" value={form.contextWindow} onChange={(event) => setForm({...form, contextWindow: Number(event.target.value)})} required /></label><label><span>Max output</span><input type="number" min="128" max="131072" value={form.maxTokens} onChange={(event) => setForm({...form, maxTokens: Number(event.target.value)})} required /></label></div><div className="provider-toggles"><label><input type="checkbox" checked={form.reasoning} onChange={(event) => setForm({...form, reasoning: event.target.checked})} /><span>Reasoning</span></label><label><input type="checkbox" checked={form.image} onChange={(event) => setForm({...form, image: event.target.checked})} /><span>Image input</span></label><label><input type="checkbox" checked={form.authHeader} onChange={(event) => setForm({...form, authHeader: event.target.checked})} /><span>Authorization header</span></label></div><label><span>API key</span><input ref={apiKeyRef} type="password" autoComplete="new-password" onInput={(event) => setHasKey((event.currentTarget as HTMLInputElement).value.length > 0)} required /></label>{failure && <div className="provider-failure"><AlertTriangle size={14} />{failure}</div>}<div className="modal-actions"><button type="button" className="secondary-button" onClick={() => {if (apiKeyRef.current) apiKeyRef.current.value = ''; setHasKey(false); setAdding(false);}} disabled={busy}>Cancel</button><button className="primary-button" disabled={!canSubmit || busy}>{busy ? <LoaderCircle size={15} className="spin" /> : <KeyRound size={15} />}Save on board</button></div></form> : <form className="form-grid provider-form provider-rotate-form" onSubmit={(event) => void rotate(event)}><div className="provider-rotate-context"><RefreshCw size={16} /><span><strong>Rotate key for {rotateTarget!.name || rotateTarget!.id}</strong><small>Provider metadata and model settings stay unchanged.</small></span></div><label><span>New API key</span><input ref={apiKeyRef} type="password" autoComplete="new-password" autoFocus onInput={(event) => setHasKey((event.currentTarget as HTMLInputElement).value.length > 0)} required /></label>{rotateTarget!.credentialUsers > 1 && <label className="provider-shared-confirm"><input type="checkbox" checked={confirmSharedRotation} onChange={(event) => setConfirmSharedRotation(event.target.checked)} /><span>This key is shared by {rotateTarget!.credentialUsers} providers. Rotate it for all of them.</span></label>}{failure && <div className="provider-failure"><AlertTriangle size={14} />{failure}</div>}<div className="modal-actions"><button type="button" className="secondary-button" onClick={() => {if (apiKeyRef.current) apiKeyRef.current.value = ''; setHasKey(false); setConfirmSharedRotation(false); setRotateTarget(null);}} disabled={busy}>Cancel</button><button className="primary-button" disabled={!hasKey || busy || (rotateTarget!.credentialUsers > 1 && !confirmSharedRotation)}>{busy ? <LoaderCircle size={15} className="spin" /> : <RefreshCw size={15} />}Rotate key</button></div></form>}
    {failure && !adding && !rotateTarget && <div className="provider-failure"><AlertTriangle size={14} />{failure}</div>}{removeTarget && <div className="provider-remove-confirm" role="alert"><AlertTriangle size={16} /><span><strong>Remove {removeTarget.name || removeTarget.id}?</strong><small>The provider and its unshared board credential will be removed.</small></span><button className="secondary-button" onClick={() => setRemoveTarget(null)} disabled={busy}>Cancel</button><button className="danger-button" onClick={() => void remove()} disabled={busy}>{busy && <LoaderCircle size={13} className="spin" />}Remove</button></div>}
  </section></div>;
}

function ExtensionCenterDialog({boardId, boardName, boardTarget, taskId, taskName, onClose}: {boardId: string; boardName: string; boardTarget: string; taskId: string; taskName: string; onClose: () => void}) {
  const [catalog, setCatalog] = useState<ExtensionCatalog | null>(null);
  const [loading, setLoading] = useState(true);
  const [failure, setFailure] = useState('');
  const [query, setQuery] = useState('');
  const [kind, setKind] = useState('all');
  const [showUnavailable, setShowUnavailable] = useState(false);
  const [revision, setRevision] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setFailure('');
    api.extensions(boardId, taskId).then((result) => {
      if (!cancelled) setCatalog(result);
    }).catch((reason) => {
      if (!cancelled) setFailure(friendlyError(String(reason)));
    }).finally(() => {
      if (!cancelled) setLoading(false);
    });
    return () => {cancelled = true;};
  }, [boardId, taskId, revision]);

  const target = boardTarget.trim().toLowerCase();
  const health = catalog ? extensionCatalogHealth(catalog) : null;
  const summary = catalog ? extensionCatalogSummary(catalog, target) : null;
  const matchingEntries = catalog ? filterExtensions(catalog.entries, query, kind) : [];
  const unavailableCount = matchingEntries.filter((entry) => !entry.required && extensionTargetState(entry, target).state === 'unavailable').length;
  const entries = showUnavailable || query.trim() ? matchingEntries : matchingEntries.filter((entry) => entry.required || extensionTargetState(entry, target).state !== 'unavailable');
  const kinds = catalog ? ['all', ...Array.from(new Set(catalog.entries.map((entry) => entry.kind)))] : ['all'];

  return <div className="modal-backdrop"><section className="modal extension-center-modal" role="dialog" aria-modal="true" aria-labelledby="extension-center-title"><div className="modal-header"><div><span className="modal-eyebrow">{boardName} · {taskId ? taskName || 'Selected task' : 'Global'}</span><h2 id="extension-center-title">Capabilities</h2></div><div className="modal-header-actions"><button className="icon-button" title="Refresh capabilities" onClick={() => setRevision((value) => value + 1)} disabled={loading}><RefreshCw size={16} className={loading ? 'spin' : ''} /></button><button className="icon-button" title="Close" onClick={onClose}><X size={18} /></button></div></div>
    {loading && !catalog ? <div className="extension-center-loading"><LoaderCircle size={18} className="spin" /><span>Reading the board catalog</span></div> : failure && !catalog ? <div className="extension-center-failure"><AlertTriangle size={18} /><span><strong>Catalog unavailable</strong><small>{failure}</small></span><button className="secondary-button" onClick={() => setRevision((value) => value + 1)}>Retry</button></div> : catalog && summary && health ? <>
      <div className="extension-overview"><div className={`extension-health ${health.healthy ? 'healthy' : 'warning'}`}>{health.healthy ? <ShieldCheck size={18} /> : <AlertTriangle size={18} />}<span><strong>{health.healthy ? 'Board-enforced catalog' : 'Catalog needs review'}</strong><small>{health.healthy ? `v${catalog.productVersion} · read-only inventory · ${target ? target.toUpperCase() : 'target unknown'}` : health.issues.join(' · ')}</small></span></div><div className="extension-stats"><span><strong>{summary.supported}</strong><small>For this board</small></span><span><strong>{summary.required}</strong><small>Required</small></span><span><strong>{summary.skills}</strong><small>Skills</small></span><span><strong>{summary.total}</strong><small>Total</small></span></div></div>
      {catalog.diagnostics && catalog.diagnostics.length > 0 && <div className="extension-sources" aria-label="Configured capability sources">{catalog.diagnostics.map((diagnostic) => <span key={diagnostic.source} className={`source-${diagnostic.status}`} title={diagnostic.message}>{extensionSourceIcon(diagnostic.status)}<strong>{extensionSourceLabel(diagnostic.source)}</strong><small>{extensionSourceStatus(diagnostic.status)}</small></span>)}</div>}
      <div className="extension-controls"><label className="extension-search"><Search size={14} /><input aria-label="Search capabilities" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search capabilities" /></label><div className="extension-kind-tabs" role="tablist" aria-label="Capability type">{kinds.map((value) => <button key={value} type="button" role="tab" aria-selected={kind === value} className={kind === value ? 'selected' : ''} onClick={() => setKind(value)}>{value === 'all' ? 'All' : extensionKindLabel(value)}</button>)}</div>{unavailableCount > 0 && !query.trim() && <button className={`extension-optional-toggle ${showUnavailable ? 'selected' : ''}`} type="button" aria-pressed={showUnavailable} onClick={() => setShowUnavailable((value) => !value)}>{showUnavailable ? 'Hide' : 'Show'} {unavailableCount} optional</button>}</div>
      <div className="extension-list">{entries.map((entry) => {
        const targetState = extensionTargetState(entry, target);
        const Icon = entry.kind === 'skill' ? Brain : entry.kind === 'provider' ? Bot : entry.kind === 'integration' ? Wrench : entry.kind === 'package' ? Package : entry.kind === 'prompt' ? FilePenLine : entry.kind === 'theme' ? Palette : Box;
        const permissions = entry.permissions ?? [];
        const targets = entry.targets ?? [];
        const provides = entry.provides ?? [];
        const requires = entry.requires ?? [];
        return <article className={`extension-row extension-${entry.kind} extension-${targetState.state}`} key={entry.id}><div className="extension-row-icon"><Icon size={17} /></div><div className="extension-row-main"><div className="extension-row-heading"><strong>{entry.name}</strong><span className={`extension-state state-${targetState.state}`}>{targetState.label}</span></div><p>{entry.description}</p><div className="extension-meta"><span>{extensionKindLabel(entry.kind)}</span><code>{entry.version === 'configured' || entry.version === 'declared' || entry.version === 'unversioned' ? entry.version : `v${entry.version}`}</code><span>{entry.origin}</span><span>{entry.scope}</span></div>{permissions.length > 0 && <div className="extension-permissions"><ShieldCheck size={12} /><span>{permissions.map(extensionPermissionLabel).join(' · ')}</span></div>}<details className="extension-details"><summary>Technical details<ChevronRight className="details-chevron" size={12} /></summary><div><InfoRow label="ID" value={entry.id} mono copy={entry.id} /><InfoRow label="Source" value={entry.entrypoint} mono /><InfoRow label="Runtime" value={entry.runtime} /><InfoRow label="Trust" value={entry.trust} /><InfoRow label="Targets" value={targets.length ? targets.map((value) => value.toUpperCase()).join(', ') : 'All'} />{entry.statusDetail && <InfoRow label="Evidence" value={entry.statusDetail} />}{provides.length > 0 && <InfoRow label="Provides" value={provides.join(', ')} />}{requires.length > 0 && <InfoRow label="Requires" value={requires.join(', ')} />}</div></details></div></article>;
      })}{entries.length === 0 && <div className="extension-empty"><Search size={19} /><span>No matching capabilities</span></div>}</div>
      <footer className="extension-footer"><span><ShieldCheck size={13} />Execution: {catalog.policy.executionAuthority} · permissions: {catalog.policy.permissionAuthority}{catalog.capturedAt ? ` · ${relativeTime(catalog.capturedAt)}` : ''}</span><button className="primary-button" onClick={onClose}>Done</button></footer>
    </> : null}
  </section></div>;
}

function extensionPermissionLabel(permission: string) {
  const labels: Record<string, string> = {'model-network': 'Model network', workspace: 'Workspace', subprocess: 'Commands', 'rdk-devices': 'RDK devices', 'user-state': 'User state', 'current-user': 'Current user', 'model-context': 'Model context', 'agent-tools': 'Agent tools', tui: 'Terminal UI'};
  return labels[permission] ?? permission;
}

function extensionSourceLabel(source: string) {
  const labels: Record<string, string> = {providers: 'Providers', 'managed-providers': 'Managed providers', hooks: 'Hooks', lsp: 'LSP', 'openexplorer-llm': 'OpenExplorer runtime', 'openexplorer-llm-skills': 'OpenExplorer Skills', 'pi-settings': 'Pi settings', 'user-extensions': 'User extensions', 'user-skills': 'User skills', 'shared-skills': 'Shared skills', 'user-prompts': 'User prompts', 'user-themes': 'User themes', 'project-resources': 'Project resources', 'project-settings': 'Project settings', 'project-extensions': 'Project extensions', 'project-skills': 'Project skills', 'project-prompts': 'Project prompts', 'project-themes': 'Project themes', 'project-shared-skills': 'Project shared skills'};
  return labels[source] ?? source;
}

function extensionSourceStatus(status: string) {
  const labels: Record<string, string> = {ok: 'Inspected', missing: 'Not configured', contextual: 'Select a task', untrusted: 'Not trusted', invalid: 'Invalid', unsafe: 'Unsafe file', unreadable: 'Unreadable', partial: 'Partially inspected', truncated: 'Limited'};
  return labels[status] ?? status;
}

function extensionSourceIcon(status: string) {
  if (status === 'ok') return <Check size={11} />;
  if (status === 'missing' || status === 'contextual' || status === 'untrusted') return <Info size={11} />;
  return <AlertTriangle size={11} />;
}

function ScheduleDialog({boardId, tasks, selectedTaskId, onClose}: {boardId: string; tasks: Task[]; selectedTaskId?: string; onClose: () => void}) {
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [loading, setLoading] = useState(true);
	const [mutating, setMutating] = useState(false);
  const [error, setError] = useState('');
  const [taskId, setTaskId] = useState(selectedTaskId && tasks.some((task) => task.id === selectedTaskId) ? selectedTaskId : tasks[0]?.id ?? '');
  const [name, setName] = useState('');
  const [prompt, setPrompt] = useState('');
	const [cadence, setCadence] = useState<'every' | 'once'>('every');
  const [every, setEvery] = useState('30m');
	const [at, setAt] = useState('');
	const [deleteTarget, setDeleteTarget] = useState<Schedule | null>(null);
  const load = useCallback(async () => { setLoading(true); try { setSchedules(await api.schedules(boardId, true)); setError(''); } catch (reason) { setError(friendlyError(String(reason))); } finally { setLoading(false); } }, [boardId]);
  useEffect(() => { void load(); }, [load]);
	useEffect(() => { const listener = (event: KeyboardEvent) => { if (event.key === 'Escape' && !mutating) onClose(); }; window.addEventListener('keydown', listener); return () => window.removeEventListener('keydown', listener); }, [mutating, onClose]);
  async function create(event: FormEvent) {
    event.preventDefault();
    if (!taskId || !prompt.trim()) { setError('Choose a main task and enter the scheduled prompt.'); return; }
    const atRFC3339 = cadence === 'once' ? localDateTimeRFC3339(at) : '';
    if (cadence === 'once' && (!atRFC3339 || new Date(atRFC3339).getTime() <= Date.now())) { setError('Choose a future local date and time.'); return; }
    setMutating(true);
    try { await api.createSchedule(boardId, {name: name.trim(), taskId, prompt: prompt.trim(), ...(cadence === 'once' ? {at: atRFC3339} : {every})}); setPrompt(''); setName(''); await load(); } catch (reason) { setError(friendlyError(String(reason))); } finally { setMutating(false); }
  }
  async function mutate(action: 'pause' | 'resume' | 'run' | 'delete', id: string) {
    setMutating(true);
    try { if (action === 'pause') await api.pauseSchedule(boardId, id); else if (action === 'resume') await api.resumeSchedule(boardId, id); else if (action === 'run') await api.runSchedule(boardId, id); else await api.deleteSchedule(boardId, id); setDeleteTarget(null); await load(); } catch (reason) { setError(friendlyError(String(reason))); } finally { setMutating(false); }
  }
  const disabled = loading || mutating;
  return <div className="modal-backdrop"><section className="modal schedule-modal" role="dialog" aria-modal="true" aria-labelledby="schedule-title"><div className="modal-header"><div><span className="modal-eyebrow">Board-owned automation</span><h2 id="schedule-title">Schedules</h2></div><div className="modal-header-actions"><button className="icon-button" title="Refresh schedules" disabled={disabled} onClick={() => void load()}><RefreshCw size={16} className={loading ? 'spin' : ''} /></button><button className="icon-button" title="Close" disabled={mutating} onClick={onClose}><X size={18} /></button></div></div><div className="schedule-content"><form className="schedule-form" onSubmit={create}><label>Task<select value={taskId} disabled={disabled} onChange={(event) => setTaskId(event.target.value)}>{tasks.map((task) => <option key={task.id} value={task.id}>{task.name}</option>)}</select></label><label>Schedule<select value={cadence} disabled={disabled} onChange={(event) => setCadence(event.target.value as 'every' | 'once')}><option value="every">Repeat</option><option value="once">Once</option></select></label>{cadence === 'every' ? <label className="schedule-wide">Every<select value={every} disabled={disabled} onChange={(event) => setEvery(event.target.value)}><option value="5m">5 minutes</option><option value="30m">30 minutes</option><option value="1h">1 hour</option><option value="24h">Daily</option></select></label> : <label className="schedule-wide">At<input type="datetime-local" value={at} disabled={disabled} onChange={(event) => setAt(event.target.value)} required /></label>}<label className="schedule-wide">Name<input value={name} disabled={disabled} onChange={(event) => setName(event.target.value)} placeholder="Optional" maxLength={96} /></label><label className="schedule-wide">Prompt<textarea value={prompt} disabled={disabled} onChange={(event) => setPrompt(event.target.value)} placeholder="What should this task do?" rows={3} maxLength={16384} /></label><button className="primary-button schedule-wide" disabled={disabled || !taskId || !prompt.trim()}>{mutating ? <LoaderCircle size={15} className="spin" /> : <CalendarClock size={15} />}Create schedule</button></form>{error && <div className="schedule-error"><AlertTriangle size={15} />{error}</div>}<div className="schedule-list">{!loading && schedules.length === 0 && <div className="schedule-empty">No schedules on this board.</div>}{schedules.map((schedule) => { const task = tasks.find((candidate) => candidate.id === schedule.taskId); return <article className="schedule-row" key={schedule.id}><div><strong>{schedule.name}</strong><span>{schedule.status} · {task?.name ?? schedule.taskId}</span><small>{schedule.nextRun ? `Next ${formatTime(schedule.nextRun)}` : 'No future run'}{schedule.lastRun ? ` · Last ${formatTime(schedule.lastRun)}` : ''}</small>{schedule.lastResult && <small>{schedule.lastResult}</small>}</div><div className="schedule-actions">{schedule.enabled ? <button className="secondary-button" disabled={disabled} onClick={() => void mutate('pause', schedule.id)}>Pause</button> : schedule.status === 'paused' ? <button className="secondary-button" disabled={disabled} onClick={() => void mutate('resume', schedule.id)}>Resume</button> : null}{schedule.enabled && <button className="icon-button" disabled={disabled} title="Run now" onClick={() => void mutate('run', schedule.id)}><Play size={15} /></button>}<button className="icon-button destructive" disabled={disabled} title="Delete schedule" onClick={() => setDeleteTarget(schedule)}><Trash2 size={15} /></button></div></article>;})}</div>{deleteTarget && <div className="inline-confirm schedule-delete-confirm" role="alert"><AlertTriangle size={16} /><span><strong>Delete {deleteTarget.name}?</strong><small>Future runs stop. A run already started on the board continues.</small></span><button className="secondary-button" disabled={disabled} onClick={() => setDeleteTarget(null)}>Cancel</button><button className="danger-button" disabled={disabled} onClick={() => void mutate('delete', deleteTarget.id)}>{mutating && <LoaderCircle size={13} className="spin" />}Delete</button></div>}</div><div className="schedule-footer">Schedules reuse the selected task's current model, permissions, sandbox, network, and workspace. Stopping a task does not cancel its schedule.</div></section></div>;
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
  const taskAlerts = [recovery ? {tone: 'danger', label: recovery.message} : null, task?.logTruncated ? {tone: 'warning', label: 'This long task retains its newest event history.'} : null].filter(Boolean) as Array<{tone: string; label: string}>;
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

function SupportDiagnosticsDialog({bundle, onClose}: {bundle: SupportBundle; onClose: () => void}) {
  const presentation = supportBundlePresentation(bundle);
  const findings = bundle.findings ?? [];
  const icon = presentation.tone === 'failed' ? <XCircle size={18} /> : presentation.tone === 'partial' ? <AlertTriangle size={18} /> : <ShieldCheck size={18} />;
  return <div className="modal-backdrop"><section className="modal support-modal" role="dialog" aria-modal="true" aria-labelledby="support-title">
    <div className="modal-header"><div><span className="modal-eyebrow">Private board diagnostics</span><h2 id="support-title">Diagnostic results</h2></div><button className="icon-button" title="Close" onClick={onClose}><X size={18} /></button></div>
    <div className="support-content">
      <div className={`support-summary ${presentation.tone}`}>{icon}<span><strong>{presentation.label}</strong><small>{presentation.summary}</small></span></div>
      <div className="support-counts"><span><strong>{bundle.checks.pass}</strong>Passed</span><span><strong>{bundle.checks.info ?? 0}</strong>Information</span><span><strong>{bundle.checks.warn}</strong>Warnings</span><span><strong>{bundle.checks.fail}</strong>Failed</span></div>
      <div className="support-findings">{findings.length > 0 ? findings.map((finding) => <article className={`support-finding ${finding.severity}`} key={finding.code}>{finding.severity === 'error' ? <XCircle size={15} /> : finding.severity === 'warning' ? <AlertTriangle size={15} /> : <Info size={15} />}<div><span>{finding.scope}</span><strong>{finding.title}</strong><p>{finding.summary}</p><small>{finding.action}</small></div></article>) : <div className="support-empty"><ShieldCheck size={17} /><span>No action is required by the current checks.</span></div>}</div>
      <div className="support-file"><Download size={15} /><span><strong>Saved privately</strong><small>{bundle.path}</small></span><CopyButton value={bundle.path} /></div>
      <div className="support-privacy"><ShieldCheck size={14} /><span>No conversations, prompts, tool content, credentials, project files, or raw logs are included. Review the saved file before sharing it.</span></div>
    </div>
    <div className="support-footer"><span>{formatBytes(bundle.sizeBytes)} · SHA-256 {bundle.sha256.slice(0, 12)}</span><button className="primary-button" onClick={onClose}>Done</button></div>
  </section></div>;
}

function ReadinessDiagnosticsDialog({report, loading, failure, onRefresh, onRepair, onClose}: {report: DiagnosticReport | null; loading: boolean; failure: string; onRefresh: () => void; onRepair: (action: string) => void; onClose: () => void}) {
  const tone = report?.status === 'action-required' ? 'failed' : report?.status === 'attention' ? 'partial' : 'passed';
  const icon = tone === 'failed' ? <XCircle size={18} /> : tone === 'partial' ? <AlertTriangle size={18} /> : <ShieldCheck size={18} />;
  const label = report?.status === 'action-required' ? 'Action required' : report?.status === 'attention' ? 'Needs attention' : 'Ready';
  return <div className="modal-backdrop"><section className="modal support-modal" role="dialog" aria-modal="true" aria-labelledby="readiness-title">
    <div className="modal-header"><div><span className="modal-eyebrow">Board readiness</span><h2 id="readiness-title">Installation and runtime</h2></div><div className="modal-header-actions"><button className="icon-button" title="Check again" onClick={onRefresh} disabled={loading}><RefreshCw size={16} className={loading ? 'spin' : ''} /></button><button className="icon-button" title="Close" onClick={onClose} disabled={loading}><X size={18} /></button></div></div>
    {!report && loading ? <div className="deployment-loading"><LoaderCircle size={17} className="spin" />Checking the board without changing it</div> : !report ? <div className="support-content"><div className="support-empty"><AlertTriangle size={17} /><span>{failure || 'Readiness diagnostics are unavailable for this connection. Update the board-side Hobot Code, reconnect, and check again.'}</span></div></div> : <div className="support-content">
      <div className={`support-summary ${tone}`}>{icon}<span><strong>{label}</strong><small>{report.status === 'healthy' ? 'The current installation is ready for Agent work.' : 'Review the findings and apply only the bounded repairs you approve.'}</small></span></div>
      <div className="support-counts"><span><strong>{report.summary.pass}</strong>Passed</span><span><strong>{report.summary.info}</strong>Information</span><span><strong>{report.summary.warn}</strong>Warnings</span><span><strong>{report.summary.fail}</strong>Failed</span></div>
      <div className="support-findings">{report.findings.length > 0 ? report.findings.map((finding) => <article className={`support-finding ${finding.severity}`} key={finding.code}>{finding.severity === 'error' ? <XCircle size={15} /> : finding.severity === 'warning' ? <AlertTriangle size={15} /> : <Info size={15} />}<div><span>{finding.scope}</span><strong>{finding.title}</strong><p>{finding.summary}</p><small>{finding.action}</small></div></article>) : <div className="support-empty"><ShieldCheck size={17} /><span>No action is required by the current checks.</span></div>}</div>
      {report.repairs.length > 0 && <div className="diagnostic-repairs">{report.repairs.map((repair) => <div key={repair.id} className={repair.status}><ShieldCheck size={15} /><span><strong>{repair.summary}</strong><small>{repair.reason}</small></span>{repair.status === 'available' && <button className="secondary-button" onClick={() => onRepair(repair.id)} disabled={loading}>Repair</button>}</div>)}</div>}
      <details className="diagnostic-checks"><summary>All checks<ChevronRight className="details-chevron" size={13} /></summary><div>{report.checks.map((check) => <div key={check.name} className={check.status}><span>{check.status}</span><strong>{check.name}</strong><small>{check.summary}</small></div>)}</div></details>
      <div className="support-privacy"><ShieldCheck size={14} /><span>This inspection is read-only and does not create a support file, call a model, or include conversation and project content.</span></div>
    </div>}
    <div className="support-footer"><span>{report ? `Checked ${relativeTime(report.capturedAt)}` : loading ? 'Checking' : 'Unavailable'}</span><button className="primary-button" onClick={onClose} disabled={loading}>Done</button></div>
  </section></div>;
}

function BPUBenchmarkDialog({
  boardId,
  boardName,
  snapshot,
  cwd,
  onClose,
}: {
  boardId: string;
  boardName: string;
  snapshot: SystemSnapshot | null;
  cwd: string;
  onClose: () => void;
}) {
  const [discoveredModels, setDiscoveredModels] = useState<string[]>([]);
  const [selectedModel, setSelectedModel] = useState('');
  const [customModelPath, setCustomModelPath] = useState('');
  const [coreId, setCoreId] = useState(0);
  const [threadCount, setThreadCount] = useState(1);
  const [frameCount, setFrameCount] = useState(200);
  const [modelInfo, setModelInfo] = useState<BPUModelInfo | null>(null);
  const [inspecting, setInspecting] = useState(false);
  const [inspectError, setInspectError] = useState('');
  const [benchmarking, setBenchmarking] = useState(false);
  const [benchmarkError, setBenchmarkError] = useState('');
  const [currentResult, setCurrentResult] = useState<BPUBenchmarkResult | null>(null);
  const [history, setHistory] = useState<BPUBenchmarkResult[]>([]);

  const effectivePath = (selectedModel === 'custom' ? customModelPath : selectedModel).trim();

  // Load models on mount
  useEffect(() => {
    let cancelled = false;
    api.listWorkspaceBPUModels(boardId, cwd).then((models) => {
      if (cancelled) return;
      setDiscoveredModels(models);
      if (models.length > 0) {
        setSelectedModel(models[0]);
      } else {
        setSelectedModel('/root/yolov8_640x640_nv12.bin');
      }
    }).catch(() => {
      if (!cancelled) setSelectedModel('/root/yolov8_640x640_nv12.bin');
    });
    return () => { cancelled = true; };
  }, [boardId, cwd]);

  // Inspect model when path changes
  const inspectModel = useCallback(async (path: string) => {
    if (!path || inspecting || benchmarking) return;
    setInspecting(true);
    setInspectError('');
    try {
      const info = await api.inspectBPUModel(boardId, path);
      setModelInfo(info);
    } catch (err) {
      setInspectError(friendlyError(String(err)));
      setModelInfo(null);
    } finally {
      setInspecting(false);
    }
  }, [boardId, inspecting, benchmarking]);

  useEffect(() => {
    if (effectivePath && effectivePath !== 'custom') {
      void inspectModel(effectivePath);
    }
  }, [effectivePath, inspectModel]);

  const mainRef = useRef<HTMLDivElement>(null);

  // Run benchmark
  const runBenchmark = async () => {
    if (!effectivePath || benchmarking || inspecting) return;
    setBenchmarking(true);
    setBenchmarkError('');
    mainRef.current?.scrollTo({ top: 0, behavior: 'smooth' });
    try {
      const res = await api.runBPUBenchmark(boardId, {
        modelPath: effectivePath,
        coreId,
        threadCount,
        frameCount,
      });
      setCurrentResult(res);
      setHistory((prev) => [res, ...prev.slice(0, 9)]);
      mainRef.current?.scrollTo({ top: 0, behavior: 'smooth' });
    } catch (err) {
      setBenchmarkError(friendlyError(String(err)));
    } finally {
      setBenchmarking(false);
    }
  };

  const copyReport = () => {
    if (!currentResult) return;
    const md = `### BPU Benchmark Report: ${currentResult.modelName || currentResult.modelPath}
- **Board**: ${boardName} (${snapshot?.board || 'RDK'})
- **FPS**: ${currentResult.fps.toFixed(2)}
- **Average Latency**: ${currentResult.averageLatencyMs.toFixed(2)} ms
- **Min / Max Latency**: ${currentResult.minLatencyMs.toFixed(2)} ms / ${currentResult.maxLatencyMs.toFixed(2)} ms
- **Threads / Cores**: ${currentResult.threadCount} threads / Core ${currentResult.coreId === 0 ? 'Auto' : currentResult.coreId - 1}
- **Test Frames**: ${currentResult.frameCount} frames
- **Captured At**: ${currentResult.capturedAt}`;
    void navigator.clipboard.writeText(md);
  };

  const bpuCoreCount = snapshot?.bpuCores?.length ?? (snapshot?.bpuDevices?.length ?? 1);

  return (
    <div className="modal-backdrop">
      <section className="modal bpu-modal" role="dialog" aria-modal="true" aria-labelledby="bpu-title">
        <div className="modal-header">
          <div>
            <span className="modal-eyebrow">D-Robotics Hardware Acceleration · {snapshot?.board || 'RDK'} · {bpuCoreCount}x BPU</span>
            <h2 id="bpu-title">BPU Benchmark & Model Inspector</h2>
          </div>
          <button className="icon-button" title="Close" disabled={benchmarking} onClick={onClose}><X size={18} /></button>
        </div>


        <div className="bpu-layout">
          {/* Left Sidebar: Configurations */}
          <div className="bpu-sidebar">
            <div className="bpu-sidebar-group">
              <label>Select Model File</label>
              <div className="bpu-model-list">
                {discoveredModels.map((m) => (
                  <button
                    key={m}
                    type="button"
                    className={`bpu-model-item ${selectedModel === m ? 'selected' : ''}`}
                    onClick={() => setSelectedModel(m)}
                  >
                    <Package size={14} />
                    <span title={m}>{m.split('/').pop() || m}</span>
                  </button>
                ))}
                <button
                  type="button"
                  className={`bpu-model-item ${selectedModel === 'custom' ? 'selected' : ''}`}
                  onClick={() => setSelectedModel('custom')}
                >
                  <Folder size={14} />
                  <span>Custom Path...</span>
                </button>
              </div>
              {selectedModel === 'custom' && (
                <div style={{marginTop: '6px'}}>
                  <input
                    type="text"
                    value={customModelPath}
                    onChange={(e) => setCustomModelPath(e.target.value)}
                    placeholder="/path/to/model.bin"
                    style={{width: '100%', fontSize: '12px'}}
                  />
                  <button
                    type="button"
                    className="secondary-button"
                    style={{marginTop: '6px', width: '100%'}}
                    disabled={!customModelPath.trim() || inspecting}
                    onClick={() => void inspectModel(customModelPath.trim())}
                  >
                    {inspecting ? <LoaderCircle size={14} className="spin" /> : <Search size={14} />}
                    Inspect Model
                  </button>
                </div>
              )}
            </div>

            <div className="bpu-sidebar-group">
              <label>Benchmark Settings</label>
              <div className="bpu-form-row">
                <label style={{fontSize: '11px'}}>
                  BPU Core
                  <select value={coreId} disabled={benchmarking} onChange={(e) => setCoreId(Number(e.target.value))}>
                    <option value={0}>Auto / Any</option>
                    {Array.from({length: bpuCoreCount}, (_, i) => (
                      <option key={i} value={i + 1}>Core {i}</option>
                    ))}
                  </select>
                </label>
                <label style={{fontSize: '11px'}}>
                  Threads
                  <select value={threadCount} disabled={benchmarking} onChange={(e) => setThreadCount(Number(e.target.value))}>
                    <option value={1}>1 thread (latency)</option>
                    <option value={2}>2 threads</option>
                    <option value={4}>4 threads (throughput)</option>
                    <option value={8}>8 threads (stress)</option>
                  </select>
                </label>
              </div>
              <label style={{fontSize: '11px', marginTop: '4px'}}>
                Iterations (Frames)
                <select value={frameCount} disabled={benchmarking} onChange={(e) => setFrameCount(Number(e.target.value))}>
                  <option value={100}>100 frames (~1.2s)</option>
                  <option value={200}>200 frames (~2.5s)</option>
                  <option value={500}>500 frames (~6.0s)</option>
                  <option value={1000}>1000 frames (~12.0s)</option>
                </select>
              </label>
            </div>

            <div style={{marginTop: 'auto', paddingTop: '10px'}}>
              <button
                type="button"
                className="primary-button"
                style={{width: '100%', minHeight: '38px', gap: '8px'}}
                disabled={!effectivePath || benchmarking || inspecting}
                onClick={() => void runBenchmark()}
              >
                {benchmarking ? <LoaderCircle size={16} className="spin" /> : <Gauge size={16} />}
                {benchmarking ? 'Benchmarking...' : '⚡ Run BPU Benchmark'}
              </button>
            </div>
          </div>

          {/* Right Main Dashboard */}
          <div ref={mainRef} className="bpu-main">

            {benchmarkError && (
              <div className="schedule-error" style={{margin: 0}}>
                <AlertTriangle size={16} />
                <span>{benchmarkError}</span>
              </div>
            )}
            {inspectError && (
              <div className="schedule-error">
                <AlertTriangle size={15} />
                <span>{inspectError}</span>
              </div>
            )}
            {benchmarking && (
              <div className="bpu-running-banner">
                <LoaderCircle size={20} className="spin" />
                <div>
                  <strong>Executing BPU Hardware Benchmark on {snapshot?.board || 'RDK SoC'}...</strong>
                  <span>Running {frameCount} frames ({threadCount} thread{threadCount > 1 ? 's' : ''}) on {coreId === 0 ? 'All / Auto Cores' : `Core ${coreId - 1}`}</span>
                </div>
              </div>
            )}

            {/* Top Metric Cards */}
            {currentResult && (

              <div className="bpu-hero-grid">
                <div className="bpu-metric-card accent">
                  <span className="bpu-metric-label">Inference Throughput</span>
                  <span className="bpu-metric-value">{currentResult.fps.toFixed(1)} <small style={{fontSize: '14px', fontWeight: 500}}>FPS</small></span>
                  <span className="bpu-metric-sub">{currentResult.threadCount} thread(s) · {currentResult.frameCount} frames</span>
                </div>
                <div className="bpu-metric-card">
                  <span className="bpu-metric-label">Avg Latency</span>
                  <span className="bpu-metric-value">{currentResult.averageLatencyMs.toFixed(2)} <small style={{fontSize: '14px', fontWeight: 500}}>ms</small></span>
                  <span className="bpu-metric-sub">Min {currentResult.minLatencyMs.toFixed(1)}ms · Max {currentResult.maxLatencyMs.toFixed(1)}ms</span>
                </div>
                <div className="bpu-metric-card">
                  <span className="bpu-metric-label">BPU Target Core</span>
                  <span className="bpu-metric-value" style={{fontSize: '18px', paddingTop: '4px'}}>
                    {currentResult.coreId === 0 ? 'All / Auto' : `Core ${currentResult.coreId - 1}`}
                  </span>
                  <span className="bpu-metric-sub">{snapshot?.board || 'RDK SoC'} Accelerator</span>
                </div>
                <div className="bpu-metric-card">
                  <span className="bpu-metric-label">DDR Load Time</span>
                  <span className="bpu-metric-value" style={{fontSize: '18px', paddingTop: '4px'}}>
                    {modelInfo?.loadDdrCostMs ? `${modelInfo.loadDdrCostMs.toFixed(1)} ms` : 'Cached'}
                  </span>
                  <span className="bpu-metric-sub">Runtime: {modelInfo?.hbrtVersion || 'HBRT 3.15'}</span>
                </div>
              </div>
            )}

            {/* Model & Tensor Inspection */}
            {modelInfo && (
              <div className="bpu-card-section">
                <div className="bpu-section-title">
                  <span>Model Specification ({modelInfo.modelName || modelInfo.modelFile.split('/').pop()})</span>
                  {modelInfo.targetSoc && <span className="bpu-badge">SoC: {modelInfo.targetSoc.toUpperCase()}</span>}
                </div>

                {/* Input Tensors */}
                <div className="tensor-grid">
                  {modelInfo.inputs.map((t) => (
                    <div key={`in-${t.index}`} className="tensor-card">
                      <div className="tensor-card-header">
                        <strong style={{fontSize: '12px'}}>{t.name || `Input #${t.index}`}</strong>
                        <span className="tensor-tag">INPUT {t.index}</span>
                      </div>
                      <dl className="tensor-props">
                        <dt>Shape:</dt><dd>{t.validShape}</dd>
                        <dt>Format:</dt><dd>{t.tensorType}</dd>
                        {t.tensorLayout && <><dt>Layout:</dt><dd>{t.tensorLayout}</dd></>}
                        {t.inputSource && <><dt>Source:</dt><dd>{t.inputSource}</dd></>}
                        {t.alignedBytes > 0 && <><dt>Buffer:</dt><dd>{(t.alignedBytes / 1024).toFixed(1)} KB</dd></>}
                      </dl>
                    </div>
                  ))}


                  {/* Output Tensors */}
                  {modelInfo.outputs.map((t) => (
                    <div key={`out-${t.index}`} className="tensor-card">
                      <div className="tensor-card-header">
                        <strong style={{fontSize: '12px'}}>{t.name || `Output #${t.index}`}</strong>
                        <span className="tensor-tag output">OUTPUT {t.index}</span>
                      </div>
                      <dl className="tensor-props">
                        <dt>Shape:</dt><dd>{t.validShape}</dd>
                        <dt>Type:</dt><dd>{t.tensorType}</dd>
                        <dt>Quant:</dt><dd>{t.quantiType}</dd>
                        {t.alignedBytes > 0 && <><dt>Size:</dt><dd>{(t.alignedBytes / 1024).toFixed(1)} KB</dd></>}
                      </dl>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Empty State when no model inspected and no benchmark run */}
            {!currentResult && !modelInfo && !inspecting && (
              <div className="bpu-empty-hint">
                <Gauge size={42} />
                <div>
                  <strong>No Model Selected</strong>
                  <p style={{margin: '4px 0 0', fontSize: '13px'}}>Select a model from the left sidebar to inspect tensor architecture and benchmark BPU inference speed.</p>
                </div>
              </div>
            )}

            {/* Benchmark History */}
            {history.length > 0 && (
              <div className="bpu-card-section" style={{marginTop: 'auto'}}>
                <div className="bpu-section-title">
                  <span>Session Benchmark History</span>
                  <button type="button" className="secondary-button" style={{padding: '3px 8px', fontSize: '12px'}} onClick={copyReport}>
                    <Clipboard size={12} /> Copy Markdown Report
                  </button>
                </div>
                <table className="bpu-history-table">
                  <thead>
                    <tr>
                      <th>Time</th>
                      <th>Model</th>
                      <th>Threads</th>
                      <th>Core</th>
                      <th>FPS</th>
                      <th>Avg Latency</th>
                    </tr>
                  </thead>
                  <tbody>
                    {history.map((h, idx) => (
                      <tr key={idx}>
                        <td>{formatTime(h.capturedAt)}</td>
                        <td title={h.modelPath}>{h.modelName || h.modelPath.split('/').pop()}</td>
                        <td>{h.threadCount}T</td>
                        <td>{h.coreId === 0 ? 'Auto' : `Core ${h.coreId - 1}`}</td>
                        <td><strong>{h.fps.toFixed(1)} FPS</strong></td>
                        <td>{h.averageLatencyMs.toFixed(2)} ms</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>

        <div className="support-footer">
          <span>{modelInfo ? `Inspected model: ${modelInfo.modelName || modelInfo.modelFile}` : 'BPU Engine Ready'}</span>
          <button className="primary-button" onClick={onClose} disabled={benchmarking}>Done</button>
        </div>
      </section>
    </div>
  );
}

function InspectorSection({title, children}: {title: string; children: ReactNode}) { return <section className="inspector-section"><h3>{title}</h3>{children}</section>; }


function TaskSandboxInspector({task}: {task: Task}) {
  const sandbox = task.sandbox;
  if (!sandbox) return null;
  const network = task.networkMode === 'model-only'
    ? 'Model only · tools isolated'
    : task.networkMode === 'offline'
      ? 'Offline · fully isolated'
      : 'Shared';
  return <InspectorSection title="Agent boundary"><InfoRow label="Profile" value={`${task.sandboxMode || sandbox.effective} · ${sandbox.backend}`} /><InfoRow label="File writes" value={sandbox.filesystemRestricted ? 'Restricted' : 'Host access'} /><InfoRow label="Devices" value={sandbox.devicesRestricted ? 'Minimal devices' : task.sandboxMode === 'off' ? 'Host access' : 'Board hardware'} /><InfoRow label="Privileges" value={sandbox.capabilitiesDropped ? 'Dropped' : 'Host privileges'} /><InfoRow label="Network" value={network} />{sandbox.reason && <div className="deployment-summary">{sandbox.reason}</div>}</InspectorSection>;
}
function InfoRow({label, value, mono, copy}: {label: string; value: string; mono?: boolean; copy?: string}) { return <div className="info-row"><span>{label}</span><div><strong className={mono ? 'mono' : ''}>{value}</strong>{copy && <CopyButton value={copy} />}</div></div>; }
function CopyButton({value}: {value: string}) { const [copied, setCopied] = useState(false); return <button type="button" className="copy-button" title={copied ? 'Copied' : 'Copy'} onClick={() => void navigator.clipboard.writeText(value).then(() => {setCopied(true); window.setTimeout(() => setCopied(false), 1200);})}>{copied ? <Check size={13} /> : <Clipboard size={13} />}</button>; }
function ResourceBar({label, value, detail, percent, available = false, tone = ''}: {label: string; value: string; detail?: string; percent?: number; available?: boolean; tone?: string}) {
  const bounded = Math.max(0, Math.min(100, percent ?? 0));
  return <div className={`resource-bar ${tone} ${available ? 'available' : ''}`}><div><span>{label}</span><strong>{value}</strong></div>{detail && <small>{detail}</small>}{percent !== undefined && <div className="resource-track" role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(bounded)}><i style={{width: `${bounded}%`}} /></div>}</div>;
}
function formatTime(value: string) { return new Date(value).toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'}); }
function localDateTimeRFC3339(value: string) {
  const date = new Date(value);
  if (!value || Number.isNaN(date.getTime())) return '';
  const pad = (number: number) => String(number).padStart(2, '0');
  const offset = -date.getTimezoneOffset();
  const sign = offset >= 0 ? '+' : '-';
  const absolute = Math.abs(offset);
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}${sign}${pad(Math.floor(absolute / 60))}:${pad(absolute % 60)}`;
}
function hardwareResourceLabel(value: string) { if (value === 'bpu') return 'BPU'; if (value === 'media-pipeline') return 'Media pipeline'; if (value.startsWith('camera-video')) return value.replace('camera-', '/dev/'); return value; }
function relativeTime(value: string) { const seconds = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 1000)); if (seconds < 60) return 'now'; if (seconds < 3600) return `${Math.floor(seconds / 60)}m`; if (seconds < 86_400) return `${Math.floor(seconds / 3600)}h`; return `${Math.floor(seconds / 86_400)}d`; }
export default App;
