package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var version = "dev"

func usage() {
	fmt.Fprintln(os.Stderr, `Hobot Code background task service

Usage:
  hobot daemon start|status
  hobot daemon stop|restart [--force]
  hobot bridge --stdio
  hobot task start [--name NAME] [--cwd DIR] [--approve] -- PROMPT
  hobot task list [--all]
  hobot task show TASK_ID
  hobot task logs TASK_ID [--after SEQUENCE] [--follow]
  hobot task attach TASK_ID [--after SEQUENCE]
  hobot task send TASK_ID PROMPT
  hobot task abort TASK_ID
  hobot task respond TASK_ID REQUEST_ID yes|no|cancel|VALUE
  hobot task approvals TASK_ID
  hobot task resume TASK_ID [PROMPT]
  hobot task rename TASK_ID NAME
  hobot task archive|unarchive TASK_ID
  hobot task delete TASK_ID --yes
  hobot task stop TASK_ID`)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		usage()
		return fmt.Errorf("a daemon or task command is required")
	}
	switch args[0] {
	case "serve":
		return runServer(cfg)
	case "daemon":
		return runDaemonCLI(cfg, args[1:])
	case "task":
		return runTaskCLI(cfg, args[1:])
	case "bridge":
		if len(args) != 2 || args[1] != "--stdio" {
			return fmt.Errorf("usage: hobot bridge --stdio")
		}
		return runStdioBridge(cfg)
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func runServer(cfg config) error {
	oldMask := syscall.Umask(0o077)
	defer syscall.Umask(oldMask)
	if err := preparePaths(cfg); err != nil {
		return err
	}
	server, err := newDaemonServer(cfg)
	if err != nil {
		return err
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			server.shutdown()
		case <-server.stop:
		}
	}()
	return server.serve()
}

func runDaemonCLI(cfg config, args []string) error {
	if len(args) == 0 {
		args = []string{"status"}
	}
	client := daemonClient{cfg: cfg}
	switch args[0] {
	case "start":
		if len(args) != 1 {
			return fmt.Errorf("usage: hobot daemon start")
		}
		if err := client.ensureStarted(); err != nil {
			return err
		}
		info, err := client.ping()
		if err != nil {
			return err
		}
		fmt.Printf("Hobot Code agentd is running (pid %d, protocol %d).\n", info.PID, info.Protocol)
		return nil
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("usage: hobot daemon status")
		}
		info, err := client.ping()
		if err != nil {
			if isConnectionFailure(err) {
				fmt.Println("Hobot Code agentd is not running.")
				return nil
			}
			return err
		}
		return printJSON(info)
	case "stop", "restart":
		force := len(args) == 2 && args[1] == "--force"
		if len(args) > 2 || (len(args) == 2 && !force) {
			return fmt.Errorf("usage: hobot daemon %s [--force]", args[0])
		}
		_, err := client.call("daemon.shutdown", map[string]bool{"force": force})
		if err != nil {
			if isConnectionFailure(err) && args[0] == "stop" {
				fmt.Println("Hobot Code agentd is not running.")
				return nil
			}
			if isConnectionFailure(err) && args[0] == "restart" {
				if err := startDaemon(cfg); err != nil {
					return err
				}
				fmt.Println("Started Hobot Code agentd.")
				return nil
			}
			return err
		}
		deadline := time.Now().Add(5 * time.Second)
		stopped := false
		for time.Now().Before(deadline) {
			if _, err := os.Lstat(cfg.SocketPath); errors.Is(err, os.ErrNotExist) {
				stopped = true
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !stopped {
			return fmt.Errorf("agentd did not stop within 5 seconds")
		}
		if args[0] == "restart" {
			if err := startDaemon(cfg); err != nil {
				return err
			}
			fmt.Println("Restarted Hobot Code agentd.")
		} else {
			fmt.Println("Stopped Hobot Code agentd.")
		}
		return nil
	default:
		return fmt.Errorf("unknown daemon command: %s", args[0])
	}
}

func runTaskCLI(cfg config, args []string) error {
	if len(args) == 0 {
		usage()
		return fmt.Errorf("a task command is required")
	}
	client := daemonClient{cfg: cfg}
	if err := client.ensureStarted(); err != nil {
		return err
	}
	switch args[0] {
	case "start":
		return runTaskStart(client, args[1:])
	case "list":
		includeArchived := len(args) == 2 && args[1] == "--all"
		if len(args) > 2 || (len(args) == 2 && !includeArchived) {
			return fmt.Errorf("usage: hobot task list [--all]")
		}
		return taskList(client, includeArchived)
	case "show":
		if len(args) != 2 {
			return fmt.Errorf("usage: hobot task show TASK_ID")
		}
		result, err := client.call("task.get", taskIDParams{TaskID: args[1]})
		if err != nil {
			return err
		}
		var metadata taskMetadata
		if err := json.Unmarshal(result, &metadata); err != nil {
			return err
		}
		return printJSON(metadata)
	case "logs", "attach":
		return runTaskLogs(client, args[0], args[1:])
	case "send":
		if len(args) < 3 {
			return fmt.Errorf("usage: hobot task send TASK_ID PROMPT")
		}
		message := strings.Join(args[2:], " ")
		if len(message) == 0 || len(message) > maxPromptBytes {
			return fmt.Errorf("task prompt must contain 1 to %d bytes", maxPromptBytes)
		}
		return protocolCommand(client, args[1], workerCommand("prompt", map[string]any{"message": message}))
	case "abort":
		if len(args) != 2 {
			return fmt.Errorf("usage: hobot task abort TASK_ID")
		}
		return protocolCommand(client, args[1], workerCommand("abort", nil))
	case "respond":
		return runTaskRespond(client, args[1:])
	case "approvals":
		if len(args) != 2 {
			return fmt.Errorf("usage: hobot task approvals TASK_ID")
		}
		result, err := client.call("task.approvals", taskIDParams{TaskID: args[1]})
		if err != nil {
			return err
		}
		var approvals []pendingApproval
		if err := json.Unmarshal(result, &approvals); err != nil {
			return err
		}
		return printJSON(approvals)
	case "resume":
		if len(args) < 2 {
			return fmt.Errorf("usage: hobot task resume TASK_ID [PROMPT]")
		}
		result, err := client.call("task.resume", resumeTaskParams{TaskID: args[1], Prompt: strings.Join(args[2:], " ")})
		if err != nil {
			return err
		}
		var metadata taskMetadata
		if err := json.Unmarshal(result, &metadata); err != nil {
			return err
		}
		fmt.Printf("Resumed background task %s (%s).\n", metadata.ID, metadata.Name)
		return nil
	case "rename":
		if len(args) != 3 {
			return fmt.Errorf("usage: hobot task rename TASK_ID NAME")
		}
		_, err := client.call("task.rename", renameTaskParams{TaskID: args[1], Name: args[2]})
		return err
	case "archive", "unarchive":
		if len(args) != 2 {
			return fmt.Errorf("usage: hobot task %s TASK_ID", args[0])
		}
		_, err := client.call("task.archive", archiveTaskParams{TaskID: args[1], Archive: args[0] == "archive"})
		return err
	case "delete":
		if len(args) != 3 || args[2] != "--yes" {
			return fmt.Errorf("usage: hobot task delete TASK_ID --yes")
		}
		_, err := client.call("task.delete", taskIDParams{TaskID: args[1]})
		return err
	case "stop":
		if len(args) != 2 {
			return fmt.Errorf("usage: hobot task stop TASK_ID")
		}
		_, err := client.call("task.stop", taskIDParams{TaskID: args[1]})
		return err
	default:
		return fmt.Errorf("unknown task command: %s", args[0])
	}
}

func runTaskStart(client daemonClient, args []string) error {
	flags := flag.NewFlagSet("task start", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	name := flags.String("name", "", "task name")
	cwd := flags.String("cwd", "", "working directory")
	approve := flags.Bool("approve", false, "trust project resources")
	if err := flags.Parse(args); err != nil {
		return err
	}
	promptArgs := flags.Args()
	if len(promptArgs) > 0 && promptArgs[0] == "--" {
		promptArgs = promptArgs[1:]
	}
	if len(promptArgs) == 0 {
		return fmt.Errorf("usage: hobot task start [--name NAME] [--cwd DIR] [--approve] -- PROMPT")
	}
	workingDirectory := *cwd
	if workingDirectory == "" {
		var err error
		workingDirectory, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	params := startTaskParams{Name: *name, Cwd: workingDirectory, Prompt: strings.Join(promptArgs, " "), Approve: *approve}
	result, err := client.call("task.start", params)
	if err != nil {
		return err
	}
	var metadata taskMetadata
	if err := json.Unmarshal(result, &metadata); err != nil {
		return err
	}
	fmt.Printf("Started background task %s (%s).\n", metadata.ID, metadata.Name)
	fmt.Printf("Attach: hobot task attach %s\n", metadata.ID)
	return nil
}

func runTaskLogs(client daemonClient, command string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: hobot task %s TASK_ID [--after SEQUENCE]%s", command, map[bool]string{true: " [--follow]"}[command == "logs"])
	}
	taskID := args[0]
	after := uint64(0)
	follow := command == "attach"
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--follow":
			if command != "logs" {
				return fmt.Errorf("--follow is valid only with task logs")
			}
			follow = true
		case "--after":
			if index+1 >= len(args) {
				return fmt.Errorf("--after requires a sequence")
			}
			index++
			value, err := parseAfter(args[index])
			if err != nil {
				return err
			}
			after = value
		default:
			return fmt.Errorf("unknown task log option: %s", args[index])
		}
	}
	if command == "logs" && !follow {
		return replayEventPage(client, taskID, after, 200)
	}
	return client.subscribe(taskID, after, follow, command == "attach")
}

func replayEventPage(client daemonClient, taskID string, after uint64, limit int) error {
	result, err := client.call("task.events", eventPageParams{TaskID: taskID, After: after, Limit: limit})
	if err != nil {
		return err
	}
	var page eventPage
	if err := json.Unmarshal(result, &page); err != nil {
		return err
	}
	for _, event := range page.Events {
		encoded, err := json.Marshal(event)
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
	}
	if page.HasMore {
		fmt.Fprintf(os.Stderr, "More events are available; continue with --after %d.\n", page.NextAfter)
	}
	return nil
}

func runTaskRespond(client daemonClient, args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: hobot task respond TASK_ID REQUEST_ID yes|no|cancel|VALUE")
	}
	command := map[string]any{"type": "extension_ui_response", "id": args[1]}
	value := strings.Join(args[2:], " ")
	switch strings.ToLower(value) {
	case "yes", "y":
		command["confirmed"] = true
	case "no", "n":
		command["confirmed"] = false
	case "cancel":
		command["cancelled"] = true
	default:
		command["value"] = value
	}
	return protocolCommand(client, args[0], mustJSON(command))
}

func init() {
	if value := os.Getenv("HOBOT_CODE_VERSION"); version == "dev" && value != "" {
		version = value
	}
}
