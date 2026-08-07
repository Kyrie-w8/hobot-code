package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Kyrie-w8/aster-edge/internal/app"
	"github.com/Kyrie-w8/aster-edge/internal/config"
	"github.com/Kyrie-w8/aster-edge/internal/core"
	"github.com/Kyrie-w8/aster-edge/internal/tools"
)

var Version = "dev"
var Commit = "none"

type options struct {
	configPath, providerPath, boardPath string
	yes, json                           bool
}

func Run(args []string) int {
	root := flag.NewFlagSet("aster", flag.ContinueOnError)
	root.SetOutput(os.Stderr)
	opts := options{}
	root.StringVar(&opts.configPath, "config", "", "configuration JSON (defaults are used when omitted)")
	root.StringVar(&opts.providerPath, "provider", "", "model provider overlay JSON")
	root.StringVar(&opts.boardPath, "board", "", "board profile overlay JSON")
	root.BoolVar(&opts.yes, "yes", false, "approve all write and dangerous tool calls")
	root.BoolVar(&opts.json, "json", false, "emit JSON where supported")
	version := root.Bool("version", false, "print version")
	root.Usage = usage
	globalArgs, commandArgs := splitGlobalArgs(args)
	if err := root.Parse(globalArgs); err != nil {
		return 2
	}
	if *version {
		fmt.Printf("aster %s (%s)\n", Version, Commit)
		return 0
	}
	rest := commandArgs
	command := "chat"
	if len(rest) > 0 {
		command = rest[0]
		rest = rest[1:]
	}
	if command == "help" {
		usage()
		return 0
	}
	cfg, err := config.Load(opts.configPath, opts.providerPath, opts.boardPath)
	if err != nil {
		return fail(err)
	}
	signals := []os.Signal{syscall.SIGTERM}
	if command != "chat" {
		signals = append(signals, os.Interrupt)
	}
	ctx, stop := signal.NotifyContext(context.Background(), signals...)
	defer stop()
	reader := bufio.NewReader(os.Stdin)
	var input *inputBroker
	var approvals *approvalBroker
	var approval tools.ApprovalFunc
	if command == "chat" {
		input = newInputBroker(reader)
		approvals = newApprovalBroker(opts.yes)
		approval = approvals.Approve
	} else {
		approval = lineApprovalHandler(reader, opts.yes)
	}
	runtime, err := app.New(ctx, cfg, approval)
	if err != nil {
		return fail(err)
	}
	defer runtime.Close()
	switch command {
	case "chat":
		return chat(ctx, runtime, input, approvals)
	case "run":
		return runOnce(ctx, runtime, rest, opts.json)
	case "doctor":
		return printValue(runtime.Doctor(), opts.json)
	case "tools":
		return printValue(runtime.Registry.Definitions(), opts.json)
	case "skills":
		return printValue(runtime.Catalog.List(), opts.json)
	case "sessions":
		ids, err := runtime.Store.List()
		if err != nil {
			return fail(err)
		}
		return printValue(ids, opts.json)
	case "export":
		if len(rest) != 1 {
			return fail(errors.New("usage: aster export SESSION_ID"))
		}
		b, err := runtime.Store.Export(rest[0])
		if err != nil {
			return fail(err)
		}
		fmt.Println(string(b))
		return 0
	case "serve":
		return serve(ctx, runtime)
	default:
		return fail(fmt.Errorf("unknown command %q", command))
	}
}

func splitGlobalArgs(args []string) ([]string, []string) {
	var globals, command []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--config", "--provider", "--board":
			globals = append(globals, arg)
			if i+1 < len(args) {
				i++
				globals = append(globals, args[i])
			}
		case "--yes", "--json", "--version":
			globals = append(globals, arg)
		default:
			if strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "--provider=") || strings.HasPrefix(arg, "--board=") {
				globals = append(globals, arg)
			} else {
				command = append(command, arg)
			}
		}
	}
	return globals, command
}

func usage() {
	fmt.Fprint(os.Stderr, `Aster - agentic shell for embedded Linux

Usage:
  aster [global flags] chat
  aster [global flags] run --message TEXT [--session ID]
  aster [global flags] doctor|tools|skills|sessions
  aster [global flags] export SESSION_ID
  aster [global flags] serve

Global flags:
  --config FILE   base configuration JSON
  --provider FILE model provider overlay JSON
  --board FILE    board overlay JSON
  --yes           approve write/dangerous tool calls
  --json          machine-readable output
  --version       print version
`)
}

func lineApprovalHandler(reader *bufio.Reader, yes bool) tools.ApprovalFunc {
	return func(ctx context.Context, call core.ToolCall, definition core.ToolDefinition) bool {
		if yes {
			return true
		}
		if stat, err := os.Stdin.Stat(); err != nil || stat.Mode()&os.ModeCharDevice == 0 {
			return false
		}
		b, _ := json.MarshalIndent(call.Arguments, "  ", "  ")
		fmt.Fprintf(os.Stderr, "\nApproval required: %s [%s]\n  %s\nApprove? [y/N] ", call.Name, definition.Risk, string(b))
		answerCh := make(chan string, 1)
		go func() {
			answer, _ := reader.ReadString('\n')
			answerCh <- answer
		}()
		var answer string
		select {
		case answer = <-answerCh:
		case <-ctx.Done():
			return false
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		return answer == "y" || answer == "yes"
	}
}

func runOnce(ctx context.Context, runtime *app.Runtime, args []string, jsonOutput bool) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	message := flags.String("message", "", "message to send")
	sessionID := flags.String("session", "", "session to resume")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *message == "" && flags.NArg() > 0 {
		*message = strings.Join(flags.Args(), " ")
	}
	result, err := runtime.Agent.Run(ctx, *sessionID, *message)
	if err != nil {
		return fail(err)
	}
	if jsonOutput {
		return printValue(result, true)
	}
	fmt.Println(result.Content)
	fmt.Fprintln(os.Stderr, "session:", result.SessionID)
	return 0
}

func serve(ctx context.Context, runtime *app.Runtime) int {
	token := ""
	if runtime.Config.Server.TokenEnv != "" {
		token = os.Getenv(runtime.Config.Server.TokenEnv)
	}
	mux := http.NewServeMux()
	locks := newSessionLocks()
	active := newActiveTurns()
	authorized := func(r *http.Request) bool {
		return token == "" || r.Header.Get("Authorization") == "Bearer "+token
	}
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "summary": runtime.Summary()})
	})
	mux.HandleFunc("POST /v1/chat", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		input, err := readChatInput(w, r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		release := locks.acquire(input.SessionID)
		defer release()
		turnCtx, cancel := context.WithCancel(r.Context())
		defer cancel()
		registered := ""
		result, err := runtime.Agent.RunWithEvents(turnCtx, input.SessionID, input.Message, func(event core.AgentEvent) {
			if registered == "" {
				registered = event.SessionID
				active.set(registered, cancel)
			}
		})
		if registered != "" {
			active.clear(registered, cancel)
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/chat/events", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming unsupported"})
			return
		}
		input, err := readChatInput(w, r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		release := locks.acquire(input.SessionID)
		defer release()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		turnCtx, cancel := context.WithCancel(r.Context())
		defer cancel()
		registered := ""
		_, runErr := runtime.Agent.RunWithEvents(turnCtx, input.SessionID, input.Message, func(event core.AgentEvent) {
			if registered == "" {
				registered = event.SessionID
				active.set(registered, cancel)
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Type, data)
			flusher.Flush()
		})
		if registered != "" {
			active.clear(registered, cancel)
		}
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			data, _ := json.Marshal(map[string]any{"error": runErr.Error()})
			fmt.Fprintf(w, "event: stream.error\ndata: %s\n\n", data)
			flusher.Flush()
		}
	})
	mux.HandleFunc("GET /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		ids, err := runtime.Store.List()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"sessions": ids})
	})
	mux.HandleFunc("GET /v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		data, err := runtime.Store.Export(r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
	mux.HandleFunc("POST /v1/sessions/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		if !active.cancel(r.PathValue("id")) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "no active turn"})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"cancelled": true})
	})
	server := &http.Server{Addr: runtime.Config.Server.Listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	fmt.Fprintln(os.Stderr, "Aster listening on", runtime.Config.Server.Listen)
	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fail(err)
	}
	return 0
}

type chatInput struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id"`
}

func readChatInput(w http.ResponseWriter, r *http.Request) (chatInput, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var input chatInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		return chatInput{}, err
	}
	if strings.TrimSpace(input.Message) == "" {
		return chatInput{}, errors.New("message is required")
	}
	return input, nil
}

type sessionLocks struct {
	mu    sync.Mutex
	locks map[string]*sessionLock
}

type sessionLock struct {
	mu   sync.Mutex
	refs int
}

func newSessionLocks() *sessionLocks {
	return &sessionLocks{locks: map[string]*sessionLock{}}
}

func (s *sessionLocks) acquire(sessionID string) func() {
	if sessionID == "" {
		lock := &sync.Mutex{}
		lock.Lock()
		return lock.Unlock
	}
	s.mu.Lock()
	lock := s.locks[sessionID]
	if lock == nil {
		lock = &sessionLock{}
		s.locks[sessionID] = lock
	}
	lock.refs++
	s.mu.Unlock()
	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		s.mu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(s.locks, sessionID)
		}
		s.mu.Unlock()
	}
}

type activeTurns struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func newActiveTurns() *activeTurns {
	return &activeTurns{cancels: map[string]context.CancelFunc{}}
}

func (a *activeTurns) set(sessionID string, cancel context.CancelFunc) {
	a.mu.Lock()
	a.cancels[sessionID] = cancel
	a.mu.Unlock()
}

func (a *activeTurns) clear(sessionID string, cancel context.CancelFunc) {
	a.mu.Lock()
	delete(a.cancels, sessionID)
	a.mu.Unlock()
}

func (a *activeTurns) cancel(sessionID string) bool {
	a.mu.Lock()
	cancel := a.cancels[sessionID]
	a.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func printValue(value any, jsonOutput bool) int {
	if jsonOutput {
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return fail(err)
		}
		fmt.Println(string(b))
		return 0
	}
	switch v := value.(type) {
	case []string:
		for _, item := range v {
			fmt.Println(item)
		}
	default:
		b, _ := json.MarshalIndent(value, "", "  ")
		fmt.Println(string(b))
	}
	return 0
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(err error) int { fmt.Fprintln(os.Stderr, "aster:", err); return 1 }
