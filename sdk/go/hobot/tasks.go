package hobot

import (
	"bufio"
	"context"
	"encoding/json"
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
	return client.Call(ctx, "task.command", map[string]any{
		"taskId":  taskID,
		"command": map[string]any{"id": nextCommandID(), "type": "prompt", "message": message},
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
	var task Task
	err := client.Call(ctx, "task.resume", map[string]any{"taskId": taskID, "prompt": prompt}, &task)
	return task, err
}

func (client *Client) RestartTask(ctx context.Context, taskID, prompt string) (Task, error) {
	var task Task
	err := client.Call(ctx, "task.restart", map[string]any{"taskId": taskID, "prompt": prompt}, &task)
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
	if handler == nil {
		return fmt.Errorf("event handler is required")
	}
	command := exec.CommandContext(ctx, client.config.SSHBinary, client.sshArgs()...)
	stdin, err := command.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	stderr := &boundedBuffer{maximum: maximumErrorBytes}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return err
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
		return err
	}
	_ = stdin.Close()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maximumEventBytes)
	if !scanner.Scan() {
		_ = command.Wait()
		if message := stderr.String(); message != "" {
			return fmt.Errorf("SSH subscription failed: %s", message)
		}
		return fmt.Errorf("SSH subscription closed before acknowledgement")
	}
	if err := decodeResponse(scanner.Bytes(), id, nil); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err
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
		return err
	}
	if waitErr != nil && waitErr != io.EOF {
		if message := stderr.String(); message != "" {
			return fmt.Errorf("SSH subscription closed: %s", message)
		}
		return waitErr
	}
	return nil
}

func nextCommandID() string {
	id, err := requestID()
	if err != nil {
		return "hobot-code-command"
	}
	return id
}
