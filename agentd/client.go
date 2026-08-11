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
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type daemonClient struct{ cfg config }

func (client daemonClient) call(method string, params any) (json.RawMessage, error) {
	connection, err := net.DialTimeout("unix", client.cfg.SocketPath, time.Second)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	requestID := fmt.Sprintf("cli-%d", time.Now().UnixNano())
	req := request{Protocol: protocolVersion, ID: requestID, Method: method}
	if params != nil {
		req.Params, err = json.Marshal(params)
		if err != nil {
			return nil, err
		}
	}
	if err := writeJSON(connection, req); err != nil {
		return nil, err
	}
	_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))
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

func (client daemonClient) ping() (daemonInfo, error) {
	result, err := client.call("ping", nil)
	if err != nil {
		return daemonInfo{}, err
	}
	var info daemonInfo
	if err := json.Unmarshal(result, &info); err != nil {
		return daemonInfo{}, err
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
	command.Env = os.Environ()
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
	return readBoundedLine(reader, maxRequestBytes+1024)
}

func (client daemonClient) subscribe(taskID string, after uint64, follow, human bool) error {
	connection, err := net.DialTimeout("unix", client.cfg.SocketPath, time.Second)
	if err != nil {
		return err
	}
	defer connection.Close()
	requestID := fmt.Sprintf("cli-%d", time.Now().UnixNano())
	req := request{
		Protocol: protocolVersion, ID: requestID, Method: "task.subscribe",
		Params: mustJSON(subscribeParams{TaskID: taskID, After: after, Follow: follow}),
	}
	if err := writeJSON(connection, req); err != nil {
		return err
	}
	scanner := bufio.NewScanner(connection)
	scanner.Buffer(make([]byte, 64*1024), maxRequestBytes+1024)
	first := true
	renderer := humanEventRenderer{}
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
			continue
		}
		var event taskEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return err
		}
		if human {
			renderer.render(event)
		} else {
			fmt.Println(string(line))
		}
	}
	return scanner.Err()
}

type humanEventRenderer struct{ stream string }

func (renderer *humanEventRenderer) render(record taskEvent) {
	var event map[string]any
	if json.Unmarshal(record.Event, &event) != nil {
		return
	}
	eventType, _ := event["type"].(string)
	switch eventType {
	case "message_update":
		update, _ := event["assistantMessageEvent"].(map[string]any)
		delta, _ := update["delta"].(string)
		switch update["type"] {
		case "thinking_delta":
			if renderer.stream != "thinking" {
				renderer.endStream()
				fmt.Print("[thinking] ")
				renderer.stream = "thinking"
			}
			fmt.Print(delta)
		case "text_delta":
			if renderer.stream != "text" {
				renderer.endStream()
				renderer.stream = "text"
			}
			fmt.Print(delta)
		}
	case "tool_execution_start":
		renderer.endStream()
		name, _ := event["toolName"].(string)
		if name == "" {
			name, _ = event["name"].(string)
		}
		fmt.Printf("[tool] %s\n", name)
	case "extension_ui_request":
		renderer.endStream()
		method, _ := event["method"].(string)
		if method == "confirm" || method == "select" || method == "input" || method == "editor" {
			fmt.Printf("[approval %s] %v (request %v)\n", method, event["title"], event["id"])
		}
	case "agent_settled":
		renderer.endStream()
		fmt.Print("[settled]\n")
	case "extension_error":
		renderer.endStream()
		fmt.Printf("[extension error] %v\n", event["error"])
	}
}

func (renderer *humanEventRenderer) endStream() {
	if renderer.stream != "" {
		fmt.Println()
		renderer.stream = ""
	}
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

func taskList(client daemonClient) error {
	result, err := client.call("task.list", nil)
	if err != nil {
		return err
	}
	var tasks []taskMetadata
	if err := json.Unmarshal(result, &tasks); err != nil {
		return err
	}
	if len(tasks) == 0 {
		fmt.Println("No background Hobot Code tasks.")
		return nil
	}
	fmt.Println("ID\tSTATUS\tPID\tNAME\tWORKSPACE")
	for _, current := range tasks {
		fmt.Printf("%s\t%s\t%d\t%s\t%s\n", current.ID, current.Status, current.PID, current.Name, current.Cwd)
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
