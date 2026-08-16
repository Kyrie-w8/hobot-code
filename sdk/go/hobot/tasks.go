package hobot

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

func (client *Client) Ping(ctx context.Context) (DaemonInfo, error) {
	var info DaemonInfo
	err := client.Call(ctx, "ping", struct{}{}, &info)
	return info, err
}

func (client *Client) GetCapabilities(ctx context.Context) (Capabilities, error) {
	var capabilities Capabilities
	err := client.Call(ctx, "capabilities", struct{}{}, &capabilities)
	return capabilities, err
}

func (client *Client) Extensions(ctx context.Context, taskIDs ...string) (ExtensionCatalog, error) {
	var catalog ExtensionCatalog
	if len(taskIDs) > 1 {
		return catalog, fmt.Errorf("extensions accepts at most one task ID")
	}
	taskID := ""
	if len(taskIDs) == 1 {
		taskID = taskIDs[0]
		if taskID != "" && !sdkTaskIDPattern.MatchString(taskID) {
			return catalog, fmt.Errorf("task ID is invalid")
		}
	}
	if err := client.Call(ctx, "extensions.list", map[string]string{"taskId": taskID}, &catalog); err != nil {
		return catalog, err
	}
	if err := validateExtensionCatalogResult(catalog); err != nil {
		return ExtensionCatalog{}, fmt.Errorf("invalid extension catalog: %w", err)
	}
	return catalog, nil
}

func (client *Client) SystemSnapshot(ctx context.Context) (SystemSnapshot, error) {
	var snapshot SystemSnapshot
	err := client.Call(ctx, "system.snapshot", struct{}{}, &snapshot)
	return snapshot, err
}

func (client *Client) SupportBundle(ctx context.Context, includeContent bool) (SupportBundle, error) {
	var bundle SupportBundle
	if err := client.Call(ctx, "support.bundle", map[string]bool{"includeContent": includeContent}, &bundle); err != nil {
		return bundle, err
	}
	if err := validateSupportBundle(bundle); err != nil {
		return SupportBundle{}, fmt.Errorf("board returned invalid support bundle: %w", err)
	}
	return bundle, nil
}

func (client *Client) Diagnostics(ctx context.Context) (DiagnosticReport, error) {
	var report DiagnosticReport
	if err := client.Call(ctx, "diagnostics.inspect", struct{}{}, &report); err != nil {
		return report, err
	}
	normalizeLegacyDiagnosticReport(&report)
	if err := validateDiagnosticReport(report); err != nil {
		return DiagnosticReport{}, fmt.Errorf("board returned invalid diagnostics: %w", err)
	}
	return report, nil
}

func (client *Client) RepairDiagnostics(ctx context.Context, action string, confirmed bool) (DiagnosticRepairResult, error) {
	var result DiagnosticRepairResult
	if !confirmed {
		return result, fmt.Errorf("diagnostic repair requires explicit confirmation")
	}
	if action != "private-runtime-permissions" {
		return result, fmt.Errorf("unsupported board-side diagnostic repair action")
	}
	if err := client.Call(ctx, "diagnostics.repair", map[string]any{"action": action, "confirm": true}, &result); err != nil {
		return result, err
	}
	normalizeLegacyDiagnosticReport(&result.Report)
	if err := validateDiagnosticRepairResult(result); err != nil {
		return DiagnosticRepairResult{}, fmt.Errorf("board returned invalid diagnostic repair result: %w", err)
	}
	return result, nil
}

func (client *Client) InspectDeployment(ctx context.Context, cwd string) (DeploymentInspection, error) {
	var inspection DeploymentInspection
	err := client.Call(ctx, "deployment.inspect", map[string]any{"path": cwd}, &inspection)
	return inspection, err
}

func (client *Client) StartDeployment(ctx context.Context, request StartDeploymentRequest) (Task, error) {
	var task Task
	err := client.Call(ctx, "deployment.start", request, &task)
	return task, err
}

func (client *Client) DeploymentStatus(ctx context.Context, taskID string) (DeploymentStatus, error) {
	var status DeploymentStatus
	err := client.Call(ctx, "deployment.status", map[string]any{"taskId": taskID}, &status)
	return status, err
}

func (client *Client) Tasks(ctx context.Context, includeArchived bool, cursor string, limit int) (TaskPage, error) {
	var page TaskPage
	err := client.Call(ctx, "task.page", map[string]any{
		"includeArchived": includeArchived, "cursor": cursor, "limit": limit,
	}, &page)
	return page, err
}

func (client *Client) Task(ctx context.Context, taskID string) (Task, error) {
	var task Task
	err := client.Call(ctx, "task.get", map[string]any{"taskId": taskID}, &task)
	return task, err
}

func (client *Client) StartTask(ctx context.Context, request StartTaskRequest) (Task, error) {
	var task Task
	err := client.Call(ctx, "task.start", request, &task)
	return task, err
}

func (client *Client) SendPrompt(ctx context.Context, taskID, message string) error {
	return client.SendPromptWithImages(ctx, taskID, message, nil)
}

func (client *Client) SendPromptWithImages(ctx context.Context, taskID, message string, images []ImageContent) error {
	return client.Call(ctx, "task.command", map[string]any{
		"taskId":  taskID,
		"command": map[string]any{"id": nextCommandID(), "type": "prompt", "message": message, "images": images},
	}, nil)
}

func (client *Client) SetModel(ctx context.Context, taskID, provider, modelID string) error {
	return client.Call(ctx, "task.model", map[string]any{
		"taskId": taskID, "provider": provider, "modelId": modelID,
	}, nil)
}

func (client *Client) SetPermissionMode(ctx context.Context, taskID, mode string) (Task, error) {
	var task Task
	err := client.Call(ctx, "task.permissions", map[string]any{"taskId": taskID, "mode": mode}, &task)
	return task, err
}

func (client *Client) SetSandboxMode(ctx context.Context, taskID, mode string) (Task, error) {
	var task Task
	err := client.Call(ctx, "task.sandbox", map[string]any{"taskId": taskID, "mode": mode}, &task)
	return task, err
}

func (client *Client) SetNetworkMode(ctx context.Context, taskID, mode string) (Task, error) {
	var task Task
	err := client.Call(ctx, "task.network", map[string]any{"taskId": taskID, "mode": mode}, &task)
	return task, err
}

func (client *Client) RenameTask(ctx context.Context, taskID, name string) (Task, error) {
	var task Task
	err := client.Call(ctx, "task.rename", map[string]any{"taskId": taskID, "name": name}, &task)
	return task, err
}

func (client *Client) Models(ctx context.Context) ([]ModelOption, error) {
	var models []ModelOption
	err := client.Call(ctx, "models.list", struct{}{}, &models)
	return models, err
}

func (client *Client) ModelHealth(ctx context.Context, model string, force bool) (ModelHealth, error) {
	var result ModelHealth
	err := client.Call(ctx, "models.health", map[string]any{"model": model, "force": force}, &result)
	return result, err
}

func (client *Client) ModelConformance(ctx context.Context, model string, force bool) (ModelConformance, error) {
	var result ModelConformance
	err := client.Call(ctx, "models.conformance", map[string]any{"model": model, "force": force}, &result)
	return result, err
}

func (client *Client) ModelRuntimeProbe(ctx context.Context, model string) (ModelRuntimeProbe, error) {
	var result ModelRuntimeProbe
	err := client.Call(ctx, "models.runtime-probe", map[string]any{"model": model}, &result)
	return result, err
}

func (client *Client) ModelRDKProbe(ctx context.Context, model string, profiles ...string) (ModelRDKProbe, error) {
	var result ModelRDKProbe
	provider, modelID, validModel := qualificationModelIdentity(model)
	if !validModel {
		return result, fmt.Errorf("model must use provider/model format")
	}
	if len(profiles) > 1 {
		return result, fmt.Errorf("RDK probe accepts at most one profile")
	}
	profile := "read-only-rdk-diagnostic-v1"
	if len(profiles) == 1 {
		profile = profiles[0]
	}
	if _, ok := qualificationRDKProfiles[profile]; !ok || !qualificationRDKProfileRunnable[profile] {
		return result, fmt.Errorf("RDK profile is not runnable: %s", profile)
	}
	if err := client.Call(ctx, "models.rdk-probe", map[string]any{"model": model, "profile": profile}, &result); err != nil {
		return result, err
	}
	if result.Provider != provider || result.Model != modelID || result.Profile != profile || !validQualificationRDK(result, provider, modelID) {
		return ModelRDKProbe{}, fmt.Errorf("board returned invalid RDK profile evidence")
	}
	return result, nil
}

func (client *Client) ModelRDKMatrix(ctx context.Context, model string) (ModelRDKMatrix, error) {
	provider, modelID, validModel := qualificationModelIdentity(model)
	if !validModel {
		return ModelRDKMatrix{}, fmt.Errorf("model must use provider/model format")
	}
	var raw json.RawMessage
	if err := client.Call(ctx, "models.rdk-matrix", map[string]any{"model": model}, &raw); err != nil {
		return ModelRDKMatrix{}, err
	}
	result, err := decodeModelRDKMatrix(raw)
	if err != nil {
		return ModelRDKMatrix{}, err
	}
	if result.Provider != provider || result.Model != modelID {
		return ModelRDKMatrix{}, fmt.Errorf("board returned RDK profile evidence for a different model")
	}
	return result, nil
}

func (client *Client) ModelQualification(ctx context.Context, model string) (ModelQualification, error) {
	var raw json.RawMessage
	if err := client.Call(ctx, "models.qualification", map[string]any{"model": model}, &raw); err != nil {
		return ModelQualification{}, err
	}
	return decodeModelQualification(raw)
}

func (client *Client) BrowseWorkspace(ctx context.Context, path string) (WorkspaceListing, error) {
	var listing WorkspaceListing
	err := client.Call(ctx, "workspace.list", map[string]any{"path": path}, &listing)
	return listing, err
}

func (client *Client) CreateWorkspace(ctx context.Context, parent, name string) (WorkspaceListing, error) {
	var listing WorkspaceListing
	err := client.Call(ctx, "workspace.create", map[string]any{"parent": parent, "name": name}, &listing)
	return listing, err
}

func (client *Client) WorkspaceChanges(ctx context.Context, taskID string) (WorkspaceChanges, error) {
	var changes WorkspaceChanges
	err := client.Call(ctx, "workspace.changes", map[string]any{"taskId": taskID}, &changes)
	return changes, err
}

func (client *Client) InspectWorkspaceIsolation(ctx context.Context, path string) (WorkspaceIsolation, error) {
	var inspection WorkspaceIsolation
	err := client.Call(ctx, "workspace.isolation", map[string]any{"path": path}, &inspection)
	return inspection, err
}

func (client *Client) ManagedWorktrees(ctx context.Context) (ManagedWorktreeList, error) {
	var worktrees ManagedWorktreeList
	err := client.Call(ctx, "workspace.worktrees", struct{}{}, &worktrees)
	return worktrees, err
}

func (client *Client) WorkspaceWrites(ctx context.Context) (WorkspaceWriteLeaseList, error) {
	var leases WorkspaceWriteLeaseList
	err := client.Call(ctx, "workspace.writes", struct{}{}, &leases)
	return leases, err
}

func (client *Client) CleanupWorkspace(ctx context.Context, taskID string) (WorkspaceCleanupResult, error) {
	var result WorkspaceCleanupResult
	err := client.Call(ctx, "workspace.cleanup", map[string]any{"taskId": taskID}, &result)
	return result, err
}

func (client *Client) InspectWorkspaceDelivery(ctx context.Context, taskID string) (WorkspaceDelivery, error) {
	var result WorkspaceDelivery
	err := client.Call(ctx, "workspace.delivery", map[string]any{"taskId": taskID}, &result)
	return result, err
}

func (client *Client) ApplyWorkspace(ctx context.Context, taskID, expectedDigest string) (WorkspaceApplyResult, error) {
	var result WorkspaceApplyResult
	err := client.Call(ctx, "workspace.apply", map[string]any{"taskId": taskID, "expectedDigest": expectedDigest}, &result)
	return result, err
}

func (client *Client) ForkTask(ctx context.Context, request ForkTaskRequest) (Task, error) {
	var task Task
	err := client.Call(ctx, "task.fork", request, &task)
	return task, err
}

func (client *Client) Abort(ctx context.Context, taskID string) error {
	return client.Call(ctx, "task.command", map[string]any{
		"taskId":  taskID,
		"command": map[string]any{"id": nextCommandID(), "type": "abort"},
	}, nil)
}

func (client *Client) StopTask(ctx context.Context, taskID string) error {
	return client.Call(ctx, "task.stop", map[string]any{"taskId": taskID}, nil)
}

func (client *Client) ArchiveTask(ctx context.Context, taskID string, archive bool) (Task, error) {
	var task Task
	err := client.Call(ctx, "task.archive", map[string]any{"taskId": taskID, "archive": archive}, &task)
	return task, err
}

func (client *Client) DeleteTask(ctx context.Context, taskID string) error {
	return client.Call(ctx, "task.delete", map[string]any{"taskId": taskID}, nil)
}

func (client *Client) ResumeTask(ctx context.Context, taskID, prompt string) (Task, error) {
	return client.ResumeTaskWithImages(ctx, taskID, prompt, nil)
}

func (client *Client) ResumeTaskWithImages(ctx context.Context, taskID, prompt string, images []ImageContent) (Task, error) {
	var task Task
	err := client.Call(ctx, "task.resume", map[string]any{"taskId": taskID, "prompt": prompt, "images": images}, &task)
	return task, err
}

func (client *Client) RestartTask(ctx context.Context, taskID, prompt string) (Task, error) {
	return client.RestartTaskWithImages(ctx, taskID, prompt, nil)
}

func (client *Client) RestartTaskWithImages(ctx context.Context, taskID, prompt string, images []ImageContent) (Task, error) {
	var task Task
	err := client.Call(ctx, "task.restart", map[string]any{"taskId": taskID, "prompt": prompt, "images": images}, &task)
	return task, err
}

func (client *Client) Approvals(ctx context.Context, taskID string) ([]Approval, error) {
	var approvals []Approval
	err := client.Call(ctx, "task.approvals", map[string]any{"taskId": taskID}, &approvals)
	return approvals, err
}

func (client *Client) Respond(ctx context.Context, taskID, approvalID string, response map[string]any) error {
	command := map[string]any{"type": "extension_ui_response", "id": approvalID}
	for key, value := range response {
		command[key] = value
	}
	return client.Call(ctx, "task.command", map[string]any{"taskId": taskID, "command": command}, nil)
}

func (client *Client) Events(ctx context.Context, taskID string, after uint64, limit int) (EventPage, error) {
	var page EventPage
	err := client.Call(ctx, "task.events", map[string]any{"taskId": taskID, "after": after, "limit": limit}, &page)
	return page, err
}

func (client *Client) Subscribe(ctx context.Context, taskID string, after uint64, handler func(Event) error) error {
	return client.SubscribeWithState(ctx, taskID, after, nil, handler)
}

// SubscribeWithReady reports a completed subscription handshake before event
// delivery begins. Callers can distinguish a healthy quiet stream from one
// that is still retrying after a transport interruption.
func (client *Client) SubscribeWithReady(ctx context.Context, taskID string, after uint64, ready func(), handler func(Event) error) error {
	var callback func(SubscriptionState)
	if ready != nil {
		callback = func(SubscriptionState) { ready() }
	}
	return client.SubscribeWithState(ctx, taskID, after, callback, handler)
}

// SubscribeWithState also reports the durable event range negotiated by the
// board. Clients can disclose an expired cursor instead of silently presenting
// a retained tail as complete history.
func (client *Client) SubscribeWithState(ctx context.Context, taskID string, after uint64, ready func(SubscriptionState), handler func(Event) error) error {
	if handler == nil {
		return fmt.Errorf("event handler is required")
	}
	command := exec.CommandContext(ctx, client.config.SSHBinary, client.sshArgs()...)
	stdin, err := command.StdinPipe()
	if err != nil {
		return transientSubscriptionError(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return transientSubscriptionError(err)
	}
	stderr := &boundedBuffer{maximum: maximumErrorBytes}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return transientSubscriptionError(err)
	}
	id, err := requestID()
	if err != nil {
		_ = command.Process.Kill()
		return err
	}
	request, _ := json.Marshal(map[string]any{
		"protocol": ProtocolVersion, "id": id, "method": "task.subscribe",
		"params": map[string]any{"taskId": taskID, "after": after, "follow": true},
	})
	if err := writeAll(stdin, append(request, '\n')); err != nil {
		_ = command.Process.Kill()
		return transientSubscriptionError(err)
	}
	_ = stdin.Close()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maximumEventBytes)
	if !scanner.Scan() {
		_ = command.Wait()
		if message := stderr.String(); message != "" {
			return transientSubscriptionError(fmt.Errorf("SSH subscription failed: %s", message))
		}
		return transientSubscriptionError(fmt.Errorf("SSH subscription closed before acknowledgement"))
	}
	var state SubscriptionState
	if err := decodeResponse(scanner.Bytes(), id, &state); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err
	}
	if ready != nil {
		ready(state)
	}
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return fmt.Errorf("decode task event: %w", err)
		}
		if event.Protocol != ProtocolVersion || event.TaskID != taskID {
			_ = command.Process.Kill()
			_ = command.Wait()
			return fmt.Errorf("task event envelope is invalid")
		}
		if err := handler(event); err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return err
		}
	}
	err = scanner.Err()
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return transientSubscriptionError(err)
	}
	if waitErr != nil && waitErr != io.EOF {
		if message := stderr.String(); message != "" {
			return transientSubscriptionError(fmt.Errorf("SSH subscription closed: %s", message))
		}
		return transientSubscriptionError(waitErr)
	}
	return nil
}

type subscriptionTransportError struct{ cause error }

func (err *subscriptionTransportError) Error() string { return err.cause.Error() }

func (err *subscriptionTransportError) Unwrap() error { return err.cause }

func transientSubscriptionError(err error) error {
	if err == nil {
		return nil
	}
	return &subscriptionTransportError{cause: err}
}

// IsTransientSubscriptionError identifies an SSH or stream transport failure
// that is safe to retry from the last durable event sequence.
func IsTransientSubscriptionError(err error) bool {
	var transport *subscriptionTransportError
	return errors.As(err, &transport)
}

func nextCommandID() string {
	id, err := requestID()
	if err != nil {
		return "hobot-code-command"
	}
	return id
}
