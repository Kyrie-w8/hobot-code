package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func runScheduleCLI(cfg config, args []string) error {
	if len(args) == 0 {
		printScheduleUsage(os.Stderr)
		return fmt.Errorf("a schedule command is required")
	}
	if printRequestedScheduleHelp(args, os.Stdout) {
		return nil
	}
	client := daemonClient{cfg: cfg}
	if err := client.ensureStarted(); err != nil {
		return err
	}
	return runScheduleWithClient(client, args, "")
}

type scheduleCaller interface {
	call(string, any) (json.RawMessage, error)
}

func runScheduleWithClient(client scheduleCaller, args []string, defaultTaskID string) error {
	switch args[0] {
	case "create":
		params, err := parseScheduleCreate(args[1:], defaultTaskID)
		if err != nil {
			return err
		}
		result, err := client.call("schedule.create", params)
		if err != nil {
			return err
		}
		var schedule scheduleRecord
		if err := json.Unmarshal(result, &schedule); err != nil {
			return err
		}
		return printJSON(schedule)
	case "list":
		all := len(args) == 2 && args[1] == "--all"
		if len(args) > 2 || (len(args) == 2 && !all) {
			return fmt.Errorf("usage: hobot schedule list [--all]")
		}
		result, err := client.call("schedule.list", listScheduleParams{All: all})
		if err != nil {
			return err
		}
		var schedules []scheduleRecord
		if err := json.Unmarshal(result, &schedules); err != nil {
			return err
		}
		return printJSON(schedules)
	case "show":
		if len(args) < 2 || len(args) > 3 || (len(args) == 3 && args[2] != "--details") {
			return fmt.Errorf("usage: hobot schedule show ID [--details]")
		}
		result, err := client.call("schedule.show", scheduleIDParams{ID: args[1], Details: len(args) == 3})
		if err != nil {
			return err
		}
		var schedule scheduleRecord
		if err := json.Unmarshal(result, &schedule); err != nil {
			return err
		}
		return printJSON(schedule)
	case "pause", "resume", "run", "run-now":
		if len(args) != 2 {
			return fmt.Errorf("usage: hobot schedule %s ID", args[0])
		}
		method := "schedule." + args[0]
		if args[0] == "run" {
			method = "schedule.run-now"
		}
		result, err := client.call(method, scheduleIDParams{ID: args[1]})
		if err != nil {
			return err
		}
		var schedule scheduleRecord
		if err := json.Unmarshal(result, &schedule); err != nil {
			return err
		}
		return printJSON(schedule)
	case "delete":
		if len(args) != 3 || args[2] != "--yes" {
			return fmt.Errorf("usage: hobot schedule delete ID --yes")
		}
		_, err := client.call("schedule.delete", scheduleIDParams{ID: args[1]})
		return err
	default:
		return fmt.Errorf("unknown schedule command: %s", args[0])
	}
}

const scheduleCreateUsage = "usage: hobot schedule create [--name NAME] [--task TASK_ID] (--at RFC3339 | --every DURATION) (--prompt PROMPT | -- PROMPT)"

func parseScheduleCreate(args []string, defaultTaskID string) (createScheduleParams, error) {
	separator := -1
	for index, value := range args {
		if value == "--" {
			separator = index
			break
		}
	}
	if separator == len(args)-1 {
		return createScheduleParams{}, fmt.Errorf("schedule prompt is empty; %s", scheduleCreateUsage)
	}
	optionEnd := len(args)
	if separator >= 0 {
		optionEnd = separator
	}
	values := map[string]string{"--task": strings.TrimSpace(defaultTaskID)}
	seen := map[string]bool{}
	for index := 0; index < optionEnd; {
		key := args[index]
		if key == "--cron" {
			if index+1 < optionEnd {
				return createScheduleParams{}, unsupportedCronError(args[index+1])
			}
			return createScheduleParams{}, unsupportedCronError("")
		}
		if key != "--name" && key != "--task" && key != "--at" && key != "--every" && key != "--prompt" {
			return createScheduleParams{}, fmt.Errorf("unknown schedule create option %q; %s", key, scheduleCreateUsage)
		}
		if index+1 >= optionEnd || seen[key] {
			return createScheduleParams{}, fmt.Errorf("option %s requires exactly one value; %s", key, scheduleCreateUsage)
		}
		values[key] = args[index+1]
		seen[key] = true
		index += 2
	}
	prompt := strings.TrimSpace(values["--prompt"])
	if separator >= 0 {
		if seen["--prompt"] {
			return createScheduleParams{}, fmt.Errorf("provide the prompt once with --prompt or after --, not both")
		}
		prompt = strings.TrimSpace(strings.Join(args[separator+1:], " "))
	}
	if strings.TrimSpace(values["--task"]) == "" {
		return createScheduleParams{}, fmt.Errorf("--task is required outside a running main Agent; select a task ID with `hobot task list`")
	}
	if prompt == "" {
		return createScheduleParams{}, fmt.Errorf("schedule prompt is required with --prompt or after --")
	}
	if strings.TrimSpace(values["--at"]) == "" == (strings.TrimSpace(values["--every"]) == "") {
		return createScheduleParams{}, fmt.Errorf("provide exactly one of --at or --every")
	}
	return createScheduleParams{Name: values["--name"], TaskID: values["--task"], At: values["--at"], Every: values["--every"], Prompt: prompt}, nil
}

func unsupportedCronError(expression string) error {
	expression = strings.TrimSpace(expression)
	if strings.HasPrefix(expression, "*/") && strings.HasSuffix(expression, " * * * *") {
		minutes := strings.TrimSuffix(strings.TrimPrefix(expression, "*/"), " * * * *")
		if minutes != "" {
			return fmt.Errorf("--cron is not supported because recurring schedules use fixed intervals; replace %q with --every %sm", expression, minutes)
		}
	}
	return fmt.Errorf("--cron is not supported; use --every DURATION for a fixed interval or --at RFC3339 for a one-time run")
}

func printScheduleUsage(output io.Writer) {
	fmt.Fprintln(output, `Hobot Code board-owned schedules

Usage:
  hobot schedule create [--name NAME] [--task TASK_ID] (--at RFC3339 | --every DURATION) (--prompt PROMPT | -- PROMPT)
  hobot schedule list [--all]
  hobot schedule show ID [--details]
  hobot schedule pause ID
  hobot schedule resume ID
  hobot schedule run ID
  hobot schedule delete ID --yes

Inside a running main Agent, --task defaults to the current task. External
administrative calls must provide --task. Cron expressions are not supported;
use --every for fixed intervals.

Schedules resume the existing task session and keep its current model,
permissions, sandbox, network, and workspace. Stopping a task does not cancel a
schedule; pause or delete it to stop future runs.`)
}

func printRequestedScheduleHelp(args []string, output io.Writer) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printScheduleUsage(output)
		return true
	}
	for _, value := range args[1:] {
		if value == "--help" || value == "-h" {
			if args[0] == "create" {
				fmt.Fprintln(output, scheduleCreateUsage)
				fmt.Fprintln(output, "Inside a running main Agent, --task is optional. Use --every 15m for a 15-minute fixed interval; cron expressions are not supported.")
			} else {
				fmt.Fprintf(output, "hobot schedule %s\n", args[0])
			}
			return true
		}
	}
	return false
}
