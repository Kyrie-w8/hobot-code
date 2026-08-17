package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type daemonServer struct {
	cfg           config
	manager       *taskManager
	schedules     *scheduleManager
	health        *modelHealthService
	verify        *modelConformanceService
	qualification *modelQualificationStore
	rdkMatrix     *modelRDKMatrixStore
	sandbox       sandboxCapability
	build         buildIdentity
	extensions    extensionCatalog
	egress        *modelEgressServer
	listener      *net.UnixListener
	started       time.Time
	stopOnce      sync.Once
	stop          chan struct{}
}

type daemonInfo struct {
	Version              string         `json:"version"`
	Protocol             int            `json:"protocol"`
	PID                  int            `json:"pid"`
	StartedAt            time.Time      `json:"startedAt"`
	ActiveTasks          int            `json:"activeTasks"`
	QueuedTasks          int            `json:"queuedTasks"`
	MaximumTasks         int            `json:"maximumTasks"`
	MaximumSideTasks     int            `json:"maximumSideTasks"`
	SocketPath           string         `json:"socketPath"`
	StateRoot            string         `json:"stateRoot"`
	BackgroundTasks      bool           `json:"backgroundTasks"`
	ConfigurationCurrent *bool          `json:"configurationCurrent,omitempty"`
	Capabilities         capabilityInfo `json:"capabilities"`
	Build                buildIdentity  `json:"build"`
}

func newDaemonServer(cfg config) (*daemonServer, error) {
	egress, err := newModelEgressServer(cfg)
	if err != nil {
		return nil, err
	}
	if err := egress.listen(); err != nil {
		return nil, fmt.Errorf("start model egress broker: %w", err)
	}
	manager, err := newTaskManager(cfg)
	if err != nil {
		egress.shutdown()
		return nil, err
	}
	schedules, err := newScheduleManager(cfg, manager)
	if err != nil {
		egress.shutdown()
		return nil, err
	}
	manager.schedules = schedules
	extensions, err := loadExtensionCatalog(cfg.ExtensionCatalog, version)
	if err != nil {
		schedules.shutdown()
		egress.shutdown()
		return nil, err
	}
	return &daemonServer{
		cfg: cfg, manager: manager, schedules: schedules, health: newModelHealthService(cfg.gatewayToken), verify: newModelConformanceService(cfg.gatewayToken), qualification: newModelQualificationStore(cfg), rdkMatrix: newModelRDKMatrixStore(cfg), sandbox: sandboxCapabilityStatus(cfg), build: currentBuildIdentity(), extensions: extensions, egress: egress, started: time.Now().UTC(), stop: make(chan struct{}),
	}, nil
}

func (server *daemonServer) info(clientFingerprint string) daemonInfo {
	capabilities := server.capabilities()
	info := daemonInfo{
		Version: version, Protocol: protocolVersion, PID: os.Getpid(), StartedAt: server.started,
		ActiveTasks: server.manager.activeCount(), QueuedTasks: server.manager.queuedCount(), MaximumTasks: server.cfg.MaxTasks, MaximumSideTasks: server.manager.sideTaskLimit(),
		SocketPath: server.cfg.SocketPath, StateRoot: server.cfg.StateRoot, BackgroundTasks: true,
		Capabilities: capabilities, Build: server.build,
	}
	if clientFingerprint != "" {
		current := clientFingerprint == server.cfg.ConfigFingerprint
		info.ConfigurationCurrent = &current
	}
	return info
}

func (server *daemonServer) capabilities() capabilityInfo {
	capabilities := append([]string(nil), protocolCapabilities...)
	if modelEgressAvailable(server.cfg) {
		capabilities = append(capabilities, "models.egress-broker.v1")
	}
	return capabilityInfo{
		ProtocolMin: protocolVersion, ProtocolMax: protocolVersion, EventSchema: eventSchemaVersion,
		Capabilities: capabilities, MaximumRequest: maxRequestBytes, MaximumSideTasks: server.manager.sideTaskLimit(),
		MaximumResponse: maxResponseBytes, MaximumPrompt: maxPromptBytes, MaximumTasks: server.cfg.MaxTasks,
		MaximumRetained: server.cfg.MaxRetainedTasks, Sandbox: server.sandbox,
	}
}

func (server *daemonServer) listen() error {
	if err := removeStaleSocket(server.cfg.SocketPath); err != nil {
		return err
	}
	address := &net.UnixAddr{Name: server.cfg.SocketPath, Net: "unix"}
	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		return err
	}
	if err := os.Chmod(server.cfg.SocketPath, 0o600); err != nil {
		_ = listener.Close()
		return err
	}
	server.listener = listener
	if err := writePrivateFile(server.cfg.PIDPath, []byte(fmt.Sprintf("%d\n", os.Getpid()))); err != nil {
		_ = listener.Close()
		return err
	}
	return nil
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("agentd socket path is not a socket: %s", path)
	}
	if owner, ok := fileOwner(info); ok && owner != os.Getuid() {
		return fmt.Errorf("agentd socket is owned by uid %d, expected %d", owner, os.Getuid())
	}
	connection, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return fmt.Errorf("another Hobot Code agentd is already listening: %s", path)
	}
	return os.Remove(path)
}

func (server *daemonServer) serve() error {
	go server.egress.serve()
	defer server.egress.shutdown()
	if err := server.listen(); err != nil {
		return err
	}
	defer func() {
		_ = server.listener.Close()
		_ = os.Remove(server.cfg.SocketPath)
		_ = os.Remove(server.cfg.PIDPath)
	}()
	for {
		connection, err := server.listener.AcceptUnix()
		if err != nil {
			select {
			case <-server.stop:
				return nil
			default:
				return err
			}
		}
		go server.handleConnection(connection)
	}
}

func (server *daemonServer) shutdown() {
	server.stopOnce.Do(func() {
		server.schedules.shutdown()
		server.manager.interruptAll()
		server.egress.shutdown()
		close(server.stop)
		if server.listener != nil {
			_ = server.listener.Close()
		}
	})
}

func (server *daemonServer) handleConnection(connection *net.UnixConn) {
	defer connection.Close()
	if err := verifyPeer(connection); err != nil {
		_ = writeJSON(connection, failure("", "forbidden", err))
		return
	}
	_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := readBoundedLine(connection, maxRequestBytes)
	if err != nil {
		_ = writeJSON(connection, failure("", "invalid_request", err))
		return
	}
	var req request
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		_ = writeJSON(connection, failure("", "invalid_json", err))
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		_ = writeJSON(connection, failure(req.ID, "invalid_json", fmt.Errorf("request must contain exactly one JSON object")))
		return
	}
	if err := validateRequest(req); err != nil {
		_ = writeJSON(connection, failure(req.ID, "invalid_request", err))
		return
	}
	_ = connection.SetReadDeadline(time.Time{})
	server.dispatch(connection, req)
}

func (server *daemonServer) dispatch(connection *net.UnixConn, req request) {
	if daemonMethodNeedsCurrentConfiguration(req.Method) && server.cfg.ConfigFingerprint != "" && req.ConfigFingerprint != "" && req.ConfigFingerprint != server.cfg.ConfigFingerprint {
		_ = writeJSON(connection, failure(req.ID, "configuration_changed", fmt.Errorf("Hobot Code configuration changed since agentd started; run `hobot daemon restart` before %s", req.Method)))
		return
	}
	switch req.Method {
	case "ping":
		if err := decodeParams(req.Params, &struct{}{}); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, server.info(req.ConfigFingerprint)))
	case "capabilities":
		if err := decodeParams(req.Params, &struct{}{}); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, server.capabilities()))
	case "extensions.list":
		var params struct {
			TaskID string `json:"taskId,omitempty"`
		}
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		if params.TaskID == "" {
			_ = writeJSON(connection, success(req.ID, discoverConfiguredExtensions(server.extensions)))
			return
		}
		if !taskIDPattern.MatchString(params.TaskID) {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", fmt.Errorf("taskId is invalid")))
			return
		}
		current, err := server.manager.get(params.TaskID)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_not_found", err))
			return
		}
		metadata := current.snapshot()
		_ = writeJSON(connection, success(req.ID, discoverConfiguredExtensions(server.extensions, extensionInventoryContext{Cwd: metadata.Cwd, ProjectTrusted: metadata.Approved})))
	case "models.list":
		if err := decodeParams(req.Params, &struct{}{}); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		models, err := listModels(server.cfg)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "models_list_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, models))
	case "models.health":
		var params modelHealthParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		result, err := server.health.check(server.manager, params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "models_health_failed", err))
			return
		}
		if err := server.qualification.recordHealth(result, server.build); err != nil {
			_ = writeJSON(connection, failure(req.ID, "models_qualification_write_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, result))
	case "models.conformance":
		var params modelConformanceParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		result, err := server.verify.check(server.manager, params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "models_conformance_failed", err))
			return
		}
		if err := server.qualification.recordConformance(result, server.build); err != nil {
			_ = writeJSON(connection, failure(req.ID, "models_qualification_write_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, result))
	case "models.runtime-probe":
		var params modelRuntimeProbeParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		result, err := server.manager.runModelRuntimeProbe(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "models_runtime_probe_failed", err))
			return
		}
		if err := server.qualification.recordRuntime(result, server.build); err != nil {
			_ = writeJSON(connection, failure(req.ID, "models_qualification_write_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, result))
	case "models.rdk-probe":
		var params modelRDKProbeParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		result, err := server.manager.runModelRDKProbe(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "models_rdk_probe_failed", err))
			return
		}
		if err := server.rdkMatrix.record(result, server.build); err != nil {
			_ = writeJSON(connection, failure(req.ID, "models_rdk_matrix_write_failed", err))
			return
		}
		if err := server.qualification.recordRDK(result, server.build); err != nil {
			_ = writeJSON(connection, failure(req.ID, "models_qualification_write_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, result))
	case "models.rdk-matrix":
		var params modelRDKMatrixParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		result, err := server.rdkMatrix.get(server.manager, params, server.build, req.ConfigFingerprint)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "models_rdk_matrix_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, result))
	case "models.qualification":
		var params modelQualificationParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		result, err := server.qualification.get(server.manager, params, server.build, req.ConfigFingerprint)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "models_qualification_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, result))
	case "system.snapshot":
		if err := decodeParams(req.Params, &struct{}{}); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, collectSystemSnapshot(server.cfg)))
	case "diagnostics.inspect":
		if err := decodeParams(req.Params, &struct{}{}); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		report, err := server.inspectDiagnostics(req.ConfigFingerprint)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "diagnostics_inspect_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, report))
	case "diagnostics.repair":
		var params diagnosticRepairParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		result, err := server.repairDiagnostics(params, req.ConfigFingerprint)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "diagnostics_repair_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, result))
	case "support.bundle":
		var params supportBundleParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		bundle, err := server.createSupportBundle(params.IncludeContent, req.ConfigFingerprint)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "support_bundle_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, bundle))
	case "deployment.inspect":
		var params workspaceParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		inspection, err := inspectDeployment(params, collectSystemSnapshot(server.cfg))
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "deployment_inspect_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, inspection))
	case "deployment.start":
		var params deploymentStartParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		metadata, err := server.manager.startDeployment(params, collectSystemSnapshot(server.cfg))
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "deployment_start_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, metadata))
	case "deployment.status":
		var params taskIDParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		status, err := server.manager.deploymentStatus(params.TaskID)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "deployment_status_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, status))
	case "workspace.list":
		var params workspaceParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		listing, err := browseWorkspace(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "workspace_list_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, listing))
	case "workspace.create":
		var params createWorkspaceParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		listing, err := createWorkspace(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "workspace_create_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, listing))
	case "workspace.changes":
		var params workspaceChangesParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		changes, err := server.manager.workspaceChanges(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "workspace_changes_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, changes))
	case "workspace.isolation":
		var params workspaceIsolationParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		inspection, err := server.manager.inspectWorkspaceIsolation(params.Path)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "workspace_isolation_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, inspection))
	case "workspace.worktrees":
		if err := decodeParams(req.Params, &struct{}{}); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		worktrees, err := server.manager.listTaskWorktrees()
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "workspace_worktrees_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, worktrees))
	case "workspace.writes":
		if err := decodeParams(req.Params, &struct{}{}); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, map[string]any{"leases": readWorkspaceWriteLeases(server.cfg)}))
	case "workspace.cleanup":
		var params workspaceCleanupParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		result, err := server.manager.cleanupTaskWorktree(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "workspace_cleanup_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, result))
	case "workspace.delivery":
		var params workspaceDeliveryParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		result, err := server.manager.inspectWorkspaceDelivery(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "workspace_delivery_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, result))
	case "workspace.apply":
		var params workspaceApplyParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		result, err := server.manager.applyWorkspaceDelivery(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "workspace_apply_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, result))
	case "daemon.shutdown":
		var params struct {
			Force bool `json:"force,omitempty"`
		}
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		if active := server.manager.activeCount(); active > 0 && !params.Force {
			_ = writeJSON(connection, failure(req.ID, "tasks_active", fmt.Errorf("%d background task(s) are active; stop them or use --force", active)))
			return
		}
		_ = writeJSON(connection, success(req.ID, map[string]bool{"stopping": true}))
		go server.shutdown()
	case "schedule.create":
		var params createScheduleParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		result, err := server.schedules.create(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "schedule_create_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, result))
	case "schedule.list":
		var params listScheduleParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, server.schedules.list(params.All)))
	case "schedule.show":
		var params scheduleIDParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		result, err := server.schedules.show(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "schedule_not_found", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, result))
	case "schedule.pause", "schedule.resume":
		var params scheduleIDParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		var result scheduleRecord
		var err error
		if req.Method == "schedule.pause" {
			result, err = server.schedules.pause(params.ID)
		} else {
			result, err = server.schedules.resume(params.ID)
		}
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "schedule_update_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, result))
	case "schedule.delete":
		var params scheduleIDParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		if err := server.schedules.delete(params.ID); err != nil {
			_ = writeJSON(connection, failure(req.ID, "schedule_delete_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, map[string]bool{"deleted": true}))
	case "schedule.run", "schedule.run-now":
		var params scheduleIDParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		result, err := server.schedules.runNow(params.ID)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "schedule_run_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, result))
	case "task.start":
		var params startTaskParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		metadata, err := server.manager.start(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_start_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, metadata))
	case "task.list":
		if err := decodeParams(req.Params, &struct{}{}); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, server.manager.list()))
	case "task.page":
		var params pageTaskParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		page, err := server.manager.page(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_page_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, page))
	case "task.get":
		var params taskIDParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		current, err := server.manager.get(params.TaskID)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_not_found", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, current.snapshot()))
	case "task.command":
		var params commandTaskParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		current, err := server.manager.get(params.TaskID)
		if err == nil {
			err = current.sendCommand(params.Command)
		}
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_command_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, map[string]bool{"accepted": true}))
	case "task.model":
		var params setTaskModelParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		metadata, err := server.manager.setModel(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_model_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, metadata))
	case "task.approvals":
		var params taskIDParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		current, err := server.manager.get(params.TaskID)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_not_found", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, current.snapshot().Approvals))
	case "task.resume":
		var params resumeTaskParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		metadata, err := server.manager.resume(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_resume_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, metadata))
	case "task.restart":
		var params resumeTaskParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		metadata, err := server.manager.restart(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_restart_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, metadata))
	case "task.fork":
		var params forkTaskParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		metadata, err := server.manager.fork(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_fork_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, metadata))
	case "task.rename":
		var params renameTaskParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		metadata, err := server.manager.rename(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_rename_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, metadata))
	case "task.permissions":
		var params setTaskPermissionParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		metadata, err := server.manager.setPermissionMode(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_permissions_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, metadata))
	case "task.sandbox":
		var params setTaskSandboxParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		metadata, err := server.manager.setSandboxMode(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_sandbox_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, metadata))
	case "task.network":
		var params setTaskNetworkParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		metadata, err := server.manager.setNetworkMode(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_network_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, metadata))
	case "task.archive":
		var params archiveTaskParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		metadata, err := server.manager.archive(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_archive_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, metadata))
	case "task.delete":
		var params taskIDParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		if err := server.manager.delete(params.TaskID); err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_delete_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, map[string]bool{"deleted": true}))
	case "task.stop":
		var params taskIDParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		current, err := server.manager.get(params.TaskID)
		if err == nil {
			err = current.stop()
		}
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_stop_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, map[string]bool{"stopped": true}))
	case "task.subscribe":
		server.subscribe(connection, req)
	case "task.events":
		var params eventPageParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		current, err := server.manager.get(params.TaskID)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_not_found", err))
			return
		}
		if params.Limit == 0 {
			params.Limit = 200
		}
		page, err := current.eventPage(params.After, params.Limit)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "event_log_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, page))
	default:
		_ = writeJSON(connection, failure(req.ID, "method_not_found", fmt.Errorf("unknown method: %s", req.Method)))
	}
}

func (server *daemonServer) subscribe(connection *net.UnixConn, req request) {
	var params subscribeParams
	if err := decodeParams(req.Params, &params); err != nil {
		_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
		return
	}
	current, err := server.manager.get(params.TaskID)
	if err != nil {
		_ = writeJSON(connection, failure(req.ID, "task_not_found", err))
		return
	}
	page, events, cancel, err := current.subscribe(params.After, params.Follow)
	if err != nil {
		_ = writeJSON(connection, failure(req.ID, "event_log_failed", err))
		return
	}
	defer cancel()
	if err := writeJSON(connection, success(req.ID, subscriptionResult{
		Replayed: len(page.Events), Following: events != nil,
		RetainedFrom: page.RetainedFrom, RetainedThrough: page.RetainedThrough,
		LatestSequence: page.LatestSequence, HistoryTruncated: page.HistoryTruncated,
		CursorExpired: page.CursorExpired,
	})); err != nil {
		return
	}
	for _, event := range page.Events {
		if err := writeJSON(connection, event); err != nil {
			return
		}
	}
	if events == nil {
		return
	}
	for event := range events {
		if err := writeJSON(connection, event); err != nil {
			return
		}
	}
}

func readBoundedLine(reader io.Reader, maximum int) ([]byte, error) {
	return readBoundedRecord(bufio.NewReaderSize(reader, 64*1024), maximum)
}

func readBoundedRecord(buffered *bufio.Reader, maximum int) ([]byte, error) {
	var result bytes.Buffer
	for {
		fragment, prefix, err := buffered.ReadLine()
		if result.Len()+len(fragment) > maximum {
			return nil, fmt.Errorf("request exceeds %d bytes", maximum)
		}
		result.Write(fragment)
		if err != nil {
			return nil, err
		}
		if !prefix {
			return result.Bytes(), nil
		}
	}
}

func writeJSON(writer io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	for len(encoded) > 0 {
		written, err := writer.Write(encoded)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		encoded = encoded[written:]
	}
	return nil
}

func writePrivateFile(path string, content []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".agentd.*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
