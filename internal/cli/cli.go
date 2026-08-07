package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	reader := bufio.NewReader(os.Stdin)
	approval := approvalHandler(reader, opts.yes)
	runtime, err := app.New(ctx, cfg, approval)
	if err != nil {
		return fail(err)
	}
	defer runtime.Close()
	switch command {
	case "chat":
		return chat(ctx, runtime, reader)
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

func approvalHandler(reader *bufio.Reader, yes bool) tools.ApprovalFunc {
	return func(call core.ToolCall, definition core.ToolDefinition) bool {
		if yes {
			return true
		}
		if stat, err := os.Stdin.Stat(); err != nil || stat.Mode()&os.ModeCharDevice == 0 {
			return false
		}
		b, _ := json.MarshalIndent(call.Arguments, "  ", "  ")
		fmt.Fprintf(os.Stderr, "\nApproval required: %s [%s]\n  %s\nApprove? [y/N] ", call.Name, definition.Risk, string(b))
		answer, _ := reader.ReadString('\n')
		answer = strings.ToLower(strings.TrimSpace(answer))
		return answer == "y" || answer == "yes"
	}
}

func chat(ctx context.Context, runtime *app.Runtime, reader *bufio.Reader) int {
	fmt.Printf("\n\033[1;36mAster\033[0m  %s\n", runtime.Summary())
	fmt.Println("Type /help for commands. Ctrl-D exits.")
	fmt.Println()
	sessionID := ""
	for {
		fmt.Print("\033[1;32m›\033[0m ")
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			if err == io.EOF {
				fmt.Println()
				return 0
			}
			return fail(err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			done := false
			switch fields := strings.Fields(line); fields[0] {
			case "/quit", "/exit":
				return 0
			case "/help":
				fmt.Println("/new  /session  /resume ID  /tools  /skills  /doctor  /quit")
			case "/new":
				sessionID = ""
				fmt.Println("Started a new session.")
			case "/session":
				if sessionID == "" {
					fmt.Println("No session yet.")
				} else {
					fmt.Println(sessionID)
				}
			case "/resume":
				if len(fields) != 2 {
					fmt.Println("usage: /resume SESSION_ID")
				} else {
					sessionID = fields[1]
					fmt.Println("Resuming", sessionID)
				}
			case "/tools":
				printValue(runtime.Registry.Definitions(), false)
			case "/skills":
				printValue(runtime.Catalog.List(), false)
			case "/doctor":
				printValue(runtime.Doctor(), false)
			default:
				fmt.Println("Unknown command. Type /help.")
			}
			if done {
				return 0
			}
			continue
		}
		fmt.Print("\033[2mthinking...\033[0m\r")
		result, err := runtime.Agent.Run(ctx, sessionID, line)
		fmt.Print("\033[2K\r")
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			continue
		}
		sessionID = result.SessionID
		fmt.Printf("\033[1;36mAster\033[0m  %s\n\n", result.Content)
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
	var mu sync.Mutex
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "summary": runtime.Summary()})
	})
	mux.HandleFunc("POST /v1/chat", func(w http.ResponseWriter, r *http.Request) {
		if token != "" && r.Header.Get("Authorization") != "Bearer "+token {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
		var input struct {
			Message   string `json:"message"`
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		mu.Lock()
		result, err := runtime.Agent.Run(r.Context(), input.SessionID, input.Message)
		mu.Unlock()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
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
