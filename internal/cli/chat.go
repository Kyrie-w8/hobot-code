package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/Kyrie-w8/aster-edge/internal/app"
	"github.com/Kyrie-w8/aster-edge/internal/core"
)

type inputEvent struct {
	line string
	err  error
}

type inputBroker struct {
	events chan inputEvent
}

func newInputBroker(reader *bufio.Reader) *inputBroker {
	broker := &inputBroker{events: make(chan inputEvent, 16)}
	go func() {
		defer close(broker.events)
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				broker.events <- inputEvent{line: line}
			}
			if err != nil {
				broker.events <- inputEvent{err: err}
				return
			}
		}
	}()
	return broker
}

type approvalRequest struct {
	call       core.ToolCall
	definition core.ToolDefinition
	response   chan bool
}

type approvalBroker struct {
	autoApprove bool
	requests    chan approvalRequest
}

func newApprovalBroker(autoApprove bool) *approvalBroker {
	return &approvalBroker{autoApprove: autoApprove, requests: make(chan approvalRequest)}
}

func (b *approvalBroker) Approve(ctx context.Context, call core.ToolCall, definition core.ToolDefinition) bool {
	if b.autoApprove {
		return true
	}
	request := approvalRequest{call: call, definition: definition, response: make(chan bool, 1)}
	select {
	case b.requests <- request:
	case <-ctx.Done():
		return false
	}
	select {
	case approved := <-request.response:
		return approved
	case <-ctx.Done():
		return false
	}
}

type chatOptions struct {
	showThinking bool
	showDetails  bool
}

func chat(ctx context.Context, runtime *app.Runtime, input *inputBroker, approvals *approvalBroker) int {
	interrupts := make(chan os.Signal, 2)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	fmt.Printf("\n\033[1;36mAster\033[0m  %s\n", runtime.Summary())
	fmt.Println("Type /help for commands. Ctrl-C cancels a turn or exits while idle.")
	fmt.Println()

	sessionID := ""
	queued := []string{}
	options := chatOptions{showThinking: true}
	for {
		var line string
		if len(queued) > 0 {
			line = queued[0]
			queued = queued[1:]
			fmt.Printf("\033[2mRunning queued prompt (%d remaining).\033[0m\n", len(queued))
		} else {
			fmt.Print("\033[1;32m›\033[0m ")
			select {
			case <-ctx.Done():
				fmt.Println()
				return 0
			case <-interrupts:
				fmt.Println()
				return 0
			case event, ok := <-input.events:
				if !ok || errors.Is(event.err, io.EOF) {
					fmt.Println()
					return 0
				}
				if event.err != nil {
					return fail(event.err)
				}
				line = event.line
			}
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			handled, exit := handleChatCommand(runtime, line, &sessionID, &options)
			if exit {
				return 0
			}
			if handled {
				continue
			}
		}

		result, err, pending, forceExit := runInteractiveTurn(ctx, runtime, input, approvals, interrupts, sessionID, line, options)
		queued = append(queued, pending...)
		if forceExit {
			return 0
		}
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, "\033[1;31merror:\033[0m", err)
			}
			continue
		}
		sessionID = result.SessionID
	}
}

type turnResult struct {
	result core.AgentResult
	err    error
}

func runInteractiveTurn(ctx context.Context, runtime *app.Runtime, input *inputBroker, approvals *approvalBroker, interrupts <-chan os.Signal, sessionID, prompt string, options chatOptions) (core.AgentResult, error, []string, bool) {
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := make(chan core.AgentEvent, 256)
	results := make(chan turnResult, 1)
	go func() {
		result, err := runtime.Agent.RunWithEvents(turnCtx, sessionID, prompt, func(event core.AgentEvent) {
			select {
			case events <- event:
			case <-turnCtx.Done():
			}
		})
		results <- turnResult{result: result, err: err}
	}()

	renderer := newEventRenderer(options)
	queued := []string{}
	var pendingApproval *approvalRequest
	cancelRequested := false
	for {
		select {
		case <-ctx.Done():
			cancel()
			return core.AgentResult{}, ctx.Err(), queued, true
		case <-interrupts:
			if cancelRequested {
				fmt.Println("\n\033[2mForced exit.\033[0m")
				return core.AgentResult{}, context.Canceled, queued, true
			}
			cancelRequested = true
			cancel()
			if pendingApproval != nil {
				pendingApproval.response <- false
				pendingApproval = nil
			}
			fmt.Println("\n\033[2mCancelling current turn... Press Ctrl-C again to exit.\033[0m")
		case request := <-approvals.requests:
			pendingApproval = &request
			renderApproval(request)
		case inputEvent, ok := <-input.events:
			if !ok || errors.Is(inputEvent.err, io.EOF) {
				cancel()
				return core.AgentResult{}, context.Canceled, queued, true
			}
			if inputEvent.err != nil {
				cancel()
				return core.AgentResult{}, inputEvent.err, queued, false
			}
			line := strings.TrimSpace(inputEvent.line)
			if pendingApproval != nil {
				approved := line == "y" || line == "Y" || strings.EqualFold(line, "yes")
				pendingApproval.response <- approved
				pendingApproval = nil
				if approved {
					fmt.Println("\033[32mApproved once.\033[0m")
				} else {
					fmt.Println("\033[33mDenied.\033[0m")
				}
			} else if line != "" {
				queued = append(queued, line)
				fmt.Printf("\n\033[2mQueued next prompt (%d).\033[0m\n", len(queued))
			}
		case event := <-events:
			renderer.Render(event)
		case outcome := <-results:
			if pendingApproval != nil {
				pendingApproval.response <- false
			}
			for {
				select {
				case event := <-events:
					renderer.Render(event)
				default:
					renderer.Finish(outcome.err)
					return outcome.result, outcome.err, queued, false
				}
			}
		}
	}
}

func renderApproval(request approvalRequest) {
	arguments, _ := json.MarshalIndent(request.call.Arguments, "  ", "  ")
	fmt.Printf("\n\033[1;33mApproval required\033[0m  %s [%s]\n  %s\nApprove once? [y/N] ", request.call.Name, request.definition.Risk, arguments)
}

func handleChatCommand(runtime *app.Runtime, line string, sessionID *string, options *chatOptions) (bool, bool) {
	fields := strings.Fields(line)
	switch fields[0] {
	case "/q", "/quit", "/exit":
		return true, true
	case "/help":
		fmt.Println("/new  /session  /sessions  /resume ID  /models  /thinking  /details")
		fmt.Println("/tools  /skills  /doctor  /export [ID]  /clear  /q")
	case "/new":
		*sessionID = ""
		fmt.Println("Started a new session.")
	case "/session":
		if *sessionID == "" {
			fmt.Println("No session yet.")
		} else {
			fmt.Println(*sessionID)
		}
	case "/sessions":
		ids, err := runtime.Store.List()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
		} else if len(ids) == 0 {
			fmt.Println("No saved sessions.")
		} else {
			for _, id := range ids {
				marker := " "
				if id == *sessionID {
					marker = "*"
				}
				fmt.Printf("%s %s\n", marker, id)
			}
		}
	case "/resume":
		if len(fields) != 2 {
			fmt.Println("usage: /resume SESSION_ID")
		} else if _, err := runtime.Store.Records(fields[1]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
		} else {
			*sessionID = fields[1]
			fmt.Println("Resuming", *sessionID)
		}
	case "/models":
		fmt.Printf("* %s/%s\n", runtime.Config.Provider.Type, runtime.Config.Provider.Model)
	case "/thinking":
		options.showThinking = toggleOption(fields, options.showThinking)
		fmt.Println("Reasoning summaries:", onOff(options.showThinking))
	case "/details":
		options.showDetails = toggleOption(fields, options.showDetails)
		fmt.Println("Tool details:", onOff(options.showDetails))
	case "/tools":
		printValue(runtime.Registry.Definitions(), false)
	case "/skills":
		printValue(runtime.Catalog.List(), false)
	case "/doctor":
		printValue(runtime.Doctor(), false)
	case "/export":
		id := *sessionID
		if len(fields) == 2 {
			id = fields[1]
		}
		if id == "" {
			fmt.Println("No session to export.")
		} else if data, err := runtime.Store.Export(id); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
		} else {
			fmt.Println(string(data))
		}
	case "/clear":
		fmt.Print("\033[2J\033[H")
	default:
		fmt.Println("Unknown command. Type /help.")
	}
	return true, false
}

func toggleOption(fields []string, current bool) bool {
	if len(fields) == 1 {
		return !current
	}
	return strings.EqualFold(fields[1], "on") || strings.EqualFold(fields[1], "true")
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

type eventRenderer struct {
	options       chatOptions
	reasoningOpen bool
	answerOpen    bool
}

func newEventRenderer(options chatOptions) *eventRenderer {
	return &eventRenderer{options: options}
}

func (r *eventRenderer) Render(event core.AgentEvent) {
	switch event.Type {
	case core.EventProviderStarted:
		r.closeBlocks()
		fmt.Printf("\033[2mThinking with %v...\033[0m\n", event.Data["model"])
	case core.EventReasoningDelta:
		if !r.options.showThinking || event.Delta == "" {
			return
		}
		if !r.reasoningOpen {
			fmt.Print("\033[2m┌ Reasoning\n│ \033[0m")
			r.reasoningOpen = true
		}
		fmt.Print("\033[2m", event.Delta, "\033[0m")
	case core.EventTextDelta:
		if event.Delta == "" {
			return
		}
		r.closeReasoning()
		if !r.answerOpen {
			fmt.Print("\033[1;36mAster\033[0m  ")
			r.answerOpen = true
		}
		fmt.Print(event.Delta)
	case core.EventToolRequested:
		r.closeBlocks()
		fmt.Printf("\033[1;34m● %s\033[0m\n", event.ToolCall.Name)
		if r.options.showDetails {
			arguments, _ := json.MarshalIndent(event.ToolCall.Arguments, "  ", "  ")
			fmt.Printf("\033[2m%s\033[0m\n", arguments)
		}
	case core.EventToolCompleted:
		if event.Execution.OK {
			fmt.Printf("\033[32m✓ %s\033[0m \033[2m%dms\033[0m\n", event.Execution.Name, event.Execution.DurationMS)
		} else {
			fmt.Printf("\033[31m✗ %s\033[0m  %s\n", event.Execution.Name, event.Execution.Error)
		}
		if r.options.showDetails && event.Execution.Output != nil {
			output, _ := json.MarshalIndent(event.Execution.Output, "  ", "  ")
			fmt.Printf("\033[2m%s\033[0m\n", output)
		}
	}
}

func (r *eventRenderer) Finish(err error) {
	r.closeBlocks()
	if errors.Is(err, context.Canceled) {
		fmt.Println("\033[33mTurn cancelled.\033[0m")
	}
	fmt.Println()
}

func (r *eventRenderer) closeReasoning() {
	if r.reasoningOpen {
		fmt.Print("\n\033[2m└\033[0m\n")
		r.reasoningOpen = false
	}
}

func (r *eventRenderer) closeBlocks() {
	r.closeReasoning()
	if r.answerOpen {
		fmt.Println()
		r.answerOpen = false
	}
}
