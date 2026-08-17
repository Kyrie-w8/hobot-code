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
	return runScheduleWithClient(client, args)
}

type scheduleCaller interface {
	call(string, any) (json.RawMessage, error)
}

func runScheduleWithClient(client scheduleCaller, args []string) error {
	switch args[0] {
	case "create":
		params, err := parseScheduleCreate(args[1:])
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

func parseScheduleCreate(args []string) (createScheduleParams, error) {
	separator := -1
	for index, value := range args {
		if value == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator == len(args)-1 {
		return createScheduleParams{}, fmt.Errorf("usage: hobot schedule create --name NAME --task TASK_ID (--at RFC3339 | --every DURATION) -- PROMPT")
	}
	values := map[string]string{}
	for index := 0; index < separator; {
		key := args[index]
		if key != "--name" && key != "--task" && key != "--at" && key != "--every" || index+1 >= separator || values[key] != "" {
			return createScheduleParams{}, fmt.Errorf("usage: hobot schedule create --name NAME --task TASK_ID (--at RFC3339 | --every DURATION) -- PROMPT")
		}
		values[key] = args[index+1]
		index += 2
	}
	prompt := strings.Join(args[separator+1:], " ")
	if values["--name"] == "" || values["--task"] == "" || prompt == "" || (values["--at"] == "" == (values["--every"] == "")) {
		return createScheduleParams{}, fmt.Errorf("usage: hobot schedule create --name NAME --task TASK_ID (--at RFC3339 | --every DURATION) -- PROMPT")
	}
	return createScheduleParams{Name: values["--name"], TaskID: values["--task"], At: values["--at"], Every: values["--every"], Prompt: prompt}, nil
}

func printScheduleUsage(output io.Writer) {
	fmt.Fprintln(output, `Hobot Code board-owned schedules

Usage:
  hobot schedule create --name NAME --task TASK_ID (--at RFC3339 | --every DURATION) -- PROMPT
  hobot schedule list [--all]
  hobot schedule show ID [--details]
  hobot schedule pause ID
  hobot schedule resume ID
  hobot schedule run ID
  hobot schedule delete ID --yes

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
			fmt.Fprintf(output, "hobot schedule %s\n", args[0])
			return true
		}
	}
	return false
}
