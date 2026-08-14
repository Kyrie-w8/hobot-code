package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type daemonClient struct{ cfg config }

func (client daemonClient) call(method string, params any) (json.RawMessage, error) {
	if daemonMethodUsesConfigFingerprint(method) {
		info, err := client.ping()
		if err != nil {
			return nil, err
		}
		includeFingerprint := client.cfg.ConfigFingerprint != "" && containsCapability(info.Capabilities.Capabilities, "configuration.fingerprint.v1")
		return client.callInternal(method, params, includeFingerprint)
	}
	if daemonMethodNeedsCurrentConfiguration(method) {
		supported, err := client.checkConfiguration(method)
		if err != nil {
			return nil, err
		}
		return client.callInternal(method, params, supported)
	}
	return client.callUnchecked(method, params)
}

func daemonMethodUsesConfigFingerprint(method string) bool {
	switch method {
	case "diagnostics.inspect", "diagnostics.repair", "support.bundle", "models.qualification":
		return true
	default:
		return false
	}
}

func (client daemonClient) callUnchecked(method string, params any) (json.RawMessage, error) {
	return client.callInternal(method, params, false)
}

func (client daemonClient) callInternal(method string, params any, includeConfigFingerprint bool) (json.RawMessage, error) {
	connection, err := net.DialTimeout("unix", client.cfg.SocketPath, time.Second)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	requestID := fmt.Sprintf("cli-%d", time.Now().UnixNano())
	req := request{Protocol: protocolVersion, ID: requestID, Method: method}
	if includeConfigFingerprint {
		req.ConfigFingerprint = client.cfg.ConfigFingerprint
	}
	if params != nil {
		req.Params, err = json.Marshal(params)
		if err != nil {
			return nil, err
		}
	}
	if err := writeJSON(connection, req); err != nil {
		return nil, err
	}
	_ = connection.SetReadDeadline(time.Now().Add(daemonCallTimeout(method)))
	line, err := readClientLine(connection)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Protocol int             `json:"protocol"`
		ID       string          `json:"id"`
		OK       bool            `json:"ok"`
		Result   json.RawMessage `json:"result"`
		Error    *protocolError  `json:"error"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return nil, err
	}
	if envelope.Protocol != protocolVersion || envelope.ID != requestID {
		return nil, fmt.Errorf("agentd returned an invalid response envelope")
	}
	if !envelope.OK {
		if envelope.Error == nil {
			return nil, fmt.Errorf("agentd request failed")
		}
		return nil, fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
	}
	return envelope.Result, nil
}

func daemonCallTimeout(method string) time.Duration {
	if method == "models.health" {
		return modelHealthRequestTimeout + 3*time.Second
	}
	if method == "models.conformance" {
		return modelConformanceRequestTimeout + 5*time.Second
	}
	if method == "models.runtime-probe" {
		return modelRuntimeProbeTimeout + 5*time.Second
	}
	if method == "models.rdk-probe" {
		return rdkProbeTimeout + 5*time.Second
	}
	if method == "task.start" || method == "workspace.cleanup" || method == "workspace.delivery" || method == "workspace.apply" {
		return workspaceWorktreeTimeout + 10*time.Second
	}
	if method == "workspace.isolation" {
		return workspaceIsolationTimeout + 3*time.Second
	}
	return 10 * time.Second
}

func daemonMethodNeedsCurrentConfiguration(method string) bool {
	switch method {
	case "models.list", "models.health", "models.conformance", "models.runtime-probe", "models.rdk-probe", "models.rdk-matrix", "deployment.start", "task.start", "task.model", "task.resume", "task.restart", "task.fork":
		return true
	default:
		return false
	}
}

func (client daemonClient) checkConfiguration(method string) (bool, error) {
	info, err := client.ping()
	if err != nil {
		return false, err
	}
	if client.cfg.ConfigFingerprint == "" || !containsCapability(info.Capabilities.Capabilities, "configuration.fingerprint.v1") {
		return false, nil
	}
	if info.ConfigurationCurrent == nil {
		return true, fmt.Errorf("agentd advertised configuration drift detection but omitted its comparison result; run `hobot daemon restart` before %s", method)
	}
	if *info.ConfigurationCurrent {
		return true, nil
	}
	return true, fmt.Errorf("Hobot Code configuration changed since agentd started; run `hobot daemon restart` before %s", method)
}

func (client daemonClient) ping() (daemonInfo, error) {
	result, err := client.callUnchecked("ping", nil)
	if err != nil {
		return daemonInfo{}, err
	}
	var info daemonInfo
	if err := json.Unmarshal(result, &info); err != nil {
		return daemonInfo{}, err
	}
	if client.cfg.ConfigFingerprint != "" && containsCapability(info.Capabilities.Capabilities, "configuration.fingerprint.v1") {
		result, err = client.callInternal("ping", nil, true)
		if err != nil {
			return daemonInfo{}, err
		}
		if err := json.Unmarshal(result, &info); err != nil {
			return daemonInfo{}, err
		}
	}
	return info, nil
}

func (client daemonClient) ensureStarted() error {
	if _, err := client.ping(); err == nil {
		return nil
	}
	return startDaemon(client.cfg)
}

func startDaemon(cfg config) error {
	if err := preparePaths(cfg); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	command := exec.Command(executable, "serve")
	command.Env = environmentWithoutGatewayCredential(os.Environ())
	closeCredential, err := attachGatewayCredential(command, gatewayCredentialPayload(cfg))
	if err != nil {
		return err
	}
	defer closeCredential()
	command.Stdin = nil
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return err
	}
	if err := command.Process.Release(); err != nil {
		return err
	}
	client := daemonClient{cfg: cfg}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := client.ping(); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("agentd did not become ready; inspect %s", cfg.LogPath)
}

func readClientLine(reader io.Reader) ([]byte, error) {
	return readBoundedLine(reader, maxResponseBytes)
}

func (client daemonClient) subscribe(taskID string, after uint64, follow, human bool) error {
	return client.subscribeWithIO(taskID, after, follow, human, strings.NewReader(""), os.Stdout, false)
}

func (client daemonClient) subscribeWithIO(taskID string, after uint64, follow, human bool, input io.Reader, output io.Writer, interactive bool) error {
	return client.subscribeWithContext(context.Background(), taskID, after, follow, human, input, output, interactive, nil)
}

func (client daemonClient) subscribeWithContext(ctx context.Context, taskID string, after uint64, follow, human bool, input io.Reader, output io.Writer, interactive bool, progress func(uint64) error) error {
	connection, err := net.DialTimeout("unix", client.cfg.SocketPath, time.Second)
	if err != nil {
		return err
	}
	defer connection.Close()
	stopCancellation := make(chan struct{})
	defer close(stopCancellation)
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-stopCancellation:
		}
	}()
	requestID := fmt.Sprintf("cli-%d", time.Now().UnixNano())
	req := request{
		Protocol: protocolVersion, ID: requestID, Method: "task.subscribe",
		Params: mustJSON(subscribeParams{TaskID: taskID, After: after, Follow: follow}),
	}
	if err := writeJSON(connection, req); err != nil {
		return err
	}
	scanner := bufio.NewScanner(connection)
	scanner.Buffer(make([]byte, 64*1024), maxEventRecordBytes)
	first := true
	renderer := newHumanEventRendererWithContext(ctx, taskID, input, output, interactive, func(command json.RawMessage) error {
		return protocolCommand(client, taskID, command)
	})
	for scanner.Scan() {
		line := bytes.TrimSuffix(scanner.Bytes(), []byte{'\r'})
		if first {
			first = false
			var envelope response
			if err := json.Unmarshal(line, &envelope); err != nil {
				return err
			}
			if !envelope.OK {
				if envelope.Error == nil {
					return fmt.Errorf("event subscription failed")
				}
				return fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
			}
			if human {
				encoded, err := json.Marshal(envelope.Result)
				if err != nil {
					return err
				}
				var result subscriptionResult
				if err := json.Unmarshal(encoded, &result); err != nil {
					return err
				}
				if notice := eventRetentionNotice(result.RetainedFrom, result.RetainedThrough, result.LatestSequence, result.HistoryTruncated, result.CursorExpired); notice != "" {
					fmt.Fprintln(output, notice)
				}
			}
			continue
		}
		var event taskEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return err
		}
		if human {
			if err := renderer.render(event); err != nil {
				return err
			}
		} else {
			fmt.Fprintln(output, string(line))
		}
		if progress != nil {
			if err := progress(event.Sequence); err != nil {
				return err
			}
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return scanner.Err()
}

func eventRetentionNotice(retainedFrom, retainedThrough, latest uint64, truncated, expired bool) string {
	if latest > retainedThrough && truncated {
		return "Some recent task activity from the previous event log could not be recovered; new activity will continue in a fresh durable tail."
	}
	if expired && retainedFrom > 0 {
		return fmt.Sprintf("Earlier task activity is no longer retained; replay starts at event %d.", retainedFrom)
	}
	if truncated && retainedFrom > 0 {
		return fmt.Sprintf("This long task retains its newest activity; available history starts at event %d.", retainedFrom)
	}
	return ""
}

type humanEventRenderer struct {
	ctx         context.Context
	stream      string
	taskID      string
	input       *bufio.Scanner
	output      io.Writer
	interactive bool
	respond     func(json.RawMessage) error
}

func newHumanEventRenderer(taskID string, input io.Reader, output io.Writer, interactive bool, respond func(json.RawMessage) error) *humanEventRenderer {
	return newHumanEventRendererWithContext(context.Background(), taskID, input, output, interactive, respond)
}

func newHumanEventRendererWithContext(ctx context.Context, taskID string, input io.Reader, output io.Writer, interactive bool, respond func(json.RawMessage) error) *humanEventRenderer {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 1024), 16*1024)
	return &humanEventRenderer{ctx: ctx, taskID: taskID, input: scanner, output: output, interactive: interactive, respond: respond}
}

func (renderer *humanEventRenderer) render(record taskEvent) error {
	var event map[string]any
	if json.Unmarshal(record.Event, &event) != nil {
		return nil
	}
	eventType, _ := event["type"].(string)
	switch eventType {
	case "hobot_task_queued":
		renderer.endStream()
		operation, _ := event["operation"].(string)
		fmt.Fprintf(renderer.output, "[queued] %s is waiting for an Agent slot\n", safeAttachText(operation, "task", 32))
	case "hobot_task_dequeued":
		renderer.endStream()
		fmt.Fprint(renderer.output, "[starting] Agent slot acquired\n")
	case "hobot_task_queue_cancelled":
		renderer.endStream()
		fmt.Fprint(renderer.output, "[cancelled] Queued task was not started\n")
	case "hobot_task_failed", "hobot_task_interrupted":
		renderer.endStream()
		message, _ := event["message"].(string)
		recovery, _ := event["recovery"].(string)
		fmt.Fprintf(renderer.output, "[%s] %s\n", strings.TrimPrefix(eventType, "hobot_task_"), safeAttachText(message, "The task ended before completion.", 240))
		switch recovery {
		case "resume":
			fmt.Fprintf(renderer.output, "[recovery] hobot task resume %s -- 'Review the last output and continue safely.'\n", shellQuote(renderer.taskID))
		case "restart":
			fmt.Fprintf(renderer.output, "[recovery] hobot task restart %s -- 'Start this task again safely.'\n", shellQuote(renderer.taskID))
		case "check-model":
			fmt.Fprint(renderer.output, "[recovery] Check the selected model route before retrying.\n")
		case "diagnose":
			fmt.Fprint(renderer.output, "[recovery] Run `hobot diagnose` and save the private support bundle.\n")
		}
	case "hobot_task_stopped":
		renderer.endStream()
		fmt.Fprint(renderer.output, "[stopped]\n")
	case "message_update":
		update, _ := event["assistantMessageEvent"].(map[string]any)
		delta, _ := update["delta"].(string)
		switch update["type"] {
		case "thinking_delta":
			if renderer.stream != "thinking" {
				renderer.endStream()
				fmt.Fprint(renderer.output, "[thinking] ")
				renderer.stream = "thinking"
			}
			fmt.Fprint(renderer.output, delta)
		case "text_delta":
			if renderer.stream != "text" {
				renderer.endStream()
				renderer.stream = "text"
			}
			fmt.Fprint(renderer.output, delta)
		}
	case "tool_execution_start":
		renderer.endStream()
		name, _ := event["toolName"].(string)
		if name == "" {
			name, _ = event["name"].(string)
		}
		fmt.Fprintf(renderer.output, "[tool] %s\n", safeAttachText(name, "tool", 80))
	case "extension_ui_request":
		renderer.endStream()
		return renderer.renderApproval(event)
	case "agent_settled":
		renderer.endStream()
		fmt.Fprint(renderer.output, "[settled]\n")
	case "extension_error":
		renderer.endStream()
		fmt.Fprint(renderer.output, "[extension error]\n")
	}
	return nil
}

func (renderer *humanEventRenderer) endStream() {
	if renderer.stream != "" {
		fmt.Fprintln(renderer.output)
		renderer.stream = ""
	}
}

func (renderer *humanEventRenderer) renderApproval(event map[string]any) error {
	method, _ := event["method"].(string)
	if method != "confirm" && method != "select" && method != "input" && method != "editor" {
		return nil
	}
	requestID, _ := event["id"].(string)
	if requestID == "" || len(requestID) > 256 {
		fmt.Fprintln(renderer.output, "[approval] Invalid request identifier; use `hobot task approvals TASK_ID` to inspect pending requests.")
		return nil
	}
	title, _ := event["title"].(string)
	title = safeAttachText(title, "Approval required", 120)
	options := attachApprovalOptions(event["options"])
	fmt.Fprintf(renderer.output, "[approval %s] %s\n", method, title)
	if method == "select" {
		for index, option := range options {
			fmt.Fprintf(renderer.output, "  %d. %s\n", index+1, safeAttachText(option, "option", 120))
		}
	}
	if !renderer.interactive {
		renderer.printResponseCommand(requestID, method)
		return nil
	}
	response, answered := renderer.promptApproval(requestID, method, options)
	if !answered {
		renderer.printResponseCommand(requestID, method)
		return nil
	}
	if renderer.respond == nil {
		return fmt.Errorf("approval responder is unavailable")
	}
	if err := renderer.respond(response); err != nil {
		return fmt.Errorf("send approval response: %w", err)
	}
	fmt.Fprintln(renderer.output, "[approval sent]")
	return nil
}

func (renderer *humanEventRenderer) promptApproval(requestID, method string, options []string) (json.RawMessage, bool) {
	switch method {
	case "confirm":
		for {
			fmt.Fprint(renderer.output, "Allow? [y/N]: ")
			value, ok := renderer.scanInput()
			if !ok {
				return nil, false
			}
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "y", "yes":
				return approvalResponse(requestID, "confirmed", true), true
			case "", "n", "no":
				return approvalResponse(requestID, "confirmed", false), true
			default:
				fmt.Fprintln(renderer.output, "Enter y or n.")
			}
		}
	case "select":
		if len(options) == 0 {
			return nil, false
		}
		for {
			fmt.Fprintf(renderer.output, "Choose [1-%d]: ", len(options))
			value, ok := renderer.scanInput()
			if !ok {
				return nil, false
			}
			selection, err := strconv.Atoi(strings.TrimSpace(value))
			if err == nil && selection >= 1 && selection <= len(options) {
				return approvalResponse(requestID, "value", options[selection-1]), true
			}
			fmt.Fprintln(renderer.output, "Enter one of the listed numbers.")
		}
	case "input":
		fmt.Fprint(renderer.output, "Response (one line): ")
		value, ok := renderer.scanInput()
		if !ok {
			return nil, false
		}
		return approvalResponse(requestID, "value", value), true
	case "editor":
		fmt.Fprintln(renderer.output, "Enter response; finish with a line containing only a period (.).")
		lines := make([]string, 0, 8)
		for {
			value, ok := renderer.scanInput()
			if !ok {
				return nil, false
			}
			if value == "." {
				return approvalResponse(requestID, "value", strings.Join(lines, "\n")), true
			}
			lines = append(lines, value)
		}
	default:
		return nil, false
	}
}

func (renderer *humanEventRenderer) scanInput() (string, bool) {
	type scanResult struct {
		value string
		ok    bool
	}
	result := make(chan scanResult, 1)
	go func() {
		if !renderer.input.Scan() {
			result <- scanResult{}
			return
		}
		result <- scanResult{value: renderer.input.Text(), ok: true}
	}()
	select {
	case <-renderer.ctx.Done():
		return "", false
	case scanned := <-result:
		return scanned.value, scanned.ok
	}
}

func (renderer *humanEventRenderer) printResponseCommand(requestID, method string) {
	if method == "confirm" {
		fmt.Fprintf(renderer.output, "Allow: hobot task respond %s %s yes\n", shellQuote(renderer.taskID), shellQuote(requestID))
		fmt.Fprintf(renderer.output, "Deny: hobot task respond %s %s no\n", shellQuote(renderer.taskID), shellQuote(requestID))
		return
	}
	fmt.Fprintf(renderer.output, "Respond: hobot task respond %s %s VALUE\n", shellQuote(renderer.taskID), shellQuote(requestID))
}

func approvalResponse(requestID, field string, value any) json.RawMessage {
	command := map[string]any{"type": "extension_ui_response", "id": requestID, field: value}
	return mustJSON(command)
}

func attachApprovalOptions(value any) []string {
	raw, ok := value.([]any)
	if !ok || len(raw) > 50 {
		return nil
	}
	options := make([]string, 0, len(raw))
	for _, item := range raw {
		option, ok := item.(string)
		if !ok || option == "" {
			return nil
		}
		options = append(options, option)
	}
	return options
}

func safeAttachText(value, fallback string, maximumRunes int) string {
	value = strings.TrimSpace(strings.SplitN(value, "\n", 2)[0])
	value = strings.Map(func(current rune) rune {
		if current < 0x20 || current == 0x7f {
			return ' '
		}
		return current
	}, value)
	if value == "" {
		return fallback
	}
	runes := []rune(value)
	if len(runes) > maximumRunes {
		value = string(runes[:maximumRunes]) + "..."
	}
	return value
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func mustJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func parseAfter(value string) (uint64, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid event sequence: %s", value)
	}
	return parsed, nil
}

func workerCommand(commandType string, fields map[string]any) json.RawMessage {
	command := make(map[string]any, len(fields)+2)
	command["id"] = fmt.Sprintf("agentd-cli-%d", time.Now().UnixNano())
	command["type"] = commandType
	for key, value := range fields {
		command[key] = value
	}
	return mustJSON(command)
}

func printJSON(value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

func taskList(client daemonClient, includeArchived bool) error {
	var result json.RawMessage
	var err error
	if includeArchived {
		result, err = client.call("task.page", pageTaskParams{Limit: 200, IncludeArchived: true})
	} else {
		result, err = client.call("task.list", nil)
	}
	if err != nil {
		return err
	}
	var tasks []taskMetadata
	if includeArchived {
		var page taskPage
		if err := json.Unmarshal(result, &page); err != nil {
			return err
		}
		tasks = page.Tasks
	} else if err := json.Unmarshal(result, &tasks); err != nil {
		return err
	}
	if len(tasks) == 0 {
		fmt.Println("No background Hobot Code tasks.")
		return nil
	}
	fmt.Println("ID\tSTATUS\tPID\tARCHIVED\tNAME\tWORKSPACE")
	for _, current := range tasks {
		fmt.Printf("%s\t%s\t%d\t%t\t%s\t%s\n", current.ID, current.Status, current.PID, current.ArchivedAt != nil, current.Name, current.Cwd)
	}
	return nil
}

func protocolCommand(client daemonClient, taskID string, command json.RawMessage) error {
	_, err := client.call("task.command", commandTaskParams{TaskID: taskID, Command: command})
	return err
}

func isConnectionFailure(err error) bool {
	var operation *net.OpError
	return errors.As(err, &operation) || strings.Contains(err.Error(), "no such file or directory")
}

func containsCapability(capabilities []string, wanted string) bool {
	for _, capability := range capabilities {
		if capability == wanted {
			return true
		}
	}
	return false
}

func runStdioBridge(cfg config) error {
	return bridgeStreams(cfg, os.Stdin, os.Stdout)
}

func bridgeStreams(cfg config, input io.Reader, output io.Writer) error {
	client := daemonClient{cfg: cfg}
	if err := client.ensureStarted(); err != nil {
		return err
	}
	configurationChecks := false
	if info, err := client.ping(); err == nil && cfg.ConfigFingerprint != "" {
		configurationChecks = containsCapability(info.Capabilities.Capabilities, "configuration.fingerprint.v1")
	}
	reader := bufio.NewReaderSize(input, 64*1024)
	for {
		line, err := readBoundedRecord(reader, maxRequestBytes)
		if errors.Is(err, io.EOF) && len(line) == 0 {
			return nil
		}
		if err != nil {
			return err
		}
		if !json.Valid(line) {
			return fmt.Errorf("bridge input must be one valid JSON object per line")
		}
		var bridged map[string]json.RawMessage
		if err := json.Unmarshal(line, &bridged); err != nil {
			return fmt.Errorf("bridge input must be one valid JSON object per line")
		}
		if configurationChecks {
			bridged["configFingerprint"] = mustJSON(cfg.ConfigFingerprint)
		}
		line, err = json.Marshal(bridged)
		if err != nil {
			return err
		}
		connection, err := net.DialTimeout("unix", cfg.SocketPath, time.Second)
		if err != nil {
			return err
		}
		if err := writeAll(connection, append(append([]byte(nil), line...), '\n')); err != nil {
			_ = connection.Close()
			return err
		}
		if _, err := io.Copy(output, connection); err != nil {
			_ = connection.Close()
			return err
		}
		if err := connection.Close(); err != nil {
			return err
		}
	}
}

func writeAll(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		written, err := writer.Write(value)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		value = value[written:]
	}
	return nil
}
