package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"

	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bryant-w/hobot-code/sdk/go/hobot"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	requestTimeout            = 20 * time.Second
	workspaceTaskTimeout      = 3 * time.Minute
	modelHealthTimeout        = 18 * time.Second
	modelVerifyTimeout        = 55 * time.Second
	modelRuntimeTimeout       = 16 * time.Minute
	modelRDKTimeout           = 6 * time.Minute
	modelQualificationTimeout = 20 * time.Second
	deploymentStatusTimeout   = 10 * time.Minute
	deleteTimeout             = 45 * time.Second
	providerMutationTimeout   = 45 * time.Second
	boardUpdateCheckTimeout   = 15 * time.Second
	boardUpdateTimeout        = 20 * time.Minute
	boardUpdateReconnectWait  = 30 * time.Second
	maximumBoards             = 64
	maximumBoardFileSize      = 1024 * 1024
)

var (
	boardIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)
	taskIDPattern  = regexp.MustCompile(`^[0-9a-f]{24}$`)
)

type Board struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	User         string `json:"user"`
	Port         int    `json:"port"`
	IdentityFile string `json:"identityFile,omitempty"`
}

type BoardConnection struct {
	Board         Board                    `json:"board"`
	Connected     bool                     `json:"connected"`
	Reconnected   bool                     `json:"reconnected,omitempty"`
	NotInstalled  bool                     `json:"notInstalled,omitempty"`
	Daemon        *hobot.DaemonInfo        `json:"daemon,omitempty"`
	Capabilities  *hobot.Capabilities      `json:"capabilities,omitempty"`
	Snapshot      *hobot.SystemSnapshot    `json:"snapshot,omitempty"`
	Compatibility *connectionCompatibility `json:"compatibility,omitempty"`
	Error         string                   `json:"error,omitempty"`
}

type TaskEventEnvelope struct {
	BoardID string      `json:"boardId"`
	Event   hobot.Event `json:"event"`
}

type TaskWatchStatus struct {
	BoardID          string `json:"boardId"`
	TaskID           string `json:"taskId"`
	State            string `json:"state"`
	Attempt          int    `json:"attempt,omitempty"`
	Message          string `json:"message,omitempty"`
	RetainedFrom     uint64 `json:"retainedFrom,omitempty"`
	RetainedThrough  uint64 `json:"retainedThrough,omitempty"`
	LatestSequence   uint64 `json:"latestSequence,omitempty"`
	HistoryTruncated bool   `json:"historyTruncated,omitempty"`
	CursorExpired    bool   `json:"cursorExpired,omitempty"`
}

type ProviderMutationResult struct {
	Saved   bool   `json:"saved"`
	Applied bool   `json:"applied"`
	Message string `json:"message"`
}

type BoardUpdateResult struct {
	Changed          bool            `json:"changed"`
	PreviousVersion  string          `json:"previousVersion"`
	InstalledVersion string          `json:"installedVersion"`
	Message          string          `json:"message"`
	Connection       BoardConnection `json:"connection"`
}

type BoardInstallResult struct {
	Success    bool            `json:"success"`
	Message    string          `json:"message"`
	Connection BoardConnection `json:"connection"`
}

type taskWatcher struct {
	id     uint64
	cancel context.CancelFunc
}

type App struct {
	ctx context.Context

	mu          sync.Mutex
	store       *boardStore
	boards      map[string]Board
	clients     map[string]*hobot.Client
	watchers    map[string]taskWatcher
	nextWatcher uint64
}

func NewApp() *App {
	return &App{
		boards: make(map[string]Board), clients: make(map[string]*hobot.Client),
		watchers: make(map[string]taskWatcher),
	}
}

func (app *App) startup(ctx context.Context) {
	app.ctx = ctx
	store, err := newBoardStore()
	if err != nil {
		runtime.LogErrorf(ctx, "open board store: %v", err)
		return
	}
	boards, err := store.load()
	if err != nil {
		runtime.LogErrorf(ctx, "load boards: %v", err)
		return
	}
	app.mu.Lock()
	app.store = store
	for _, board := range boards {
		app.boards[board.ID] = board
	}
	app.mu.Unlock()
}

func (app *App) shutdown(context.Context) {
	app.mu.Lock()
	watchers := app.watchers
	clients := app.clients
	app.watchers = make(map[string]taskWatcher)
	app.clients = make(map[string]*hobot.Client)
	app.mu.Unlock()
	for _, watcher := range watchers {
		watcher.cancel()
	}
	for _, client := range clients {
		_ = client.Close()
	}
}

func (app *App) ListBoards() []Board {
	app.mu.Lock()
	defer app.mu.Unlock()
	return sortedBoards(app.boards)
}

func (app *App) GetAppVersion() string {
	return currentStudioVersion()
}

func (app *App) CheckBoardUpdate(boardID string) (hobot.BoardUpdateCheck, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.BoardUpdateCheck{}, err
	}
	base := app.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(base, boardUpdateCheckTimeout)
	check, err := client.CheckBoardUpdate(ctx)
	cancel()
	if err == nil || !isBoardUpdateNetworkFailure(err) {
		return check, err
	}
	infoContext, infoCancel := context.WithTimeout(base, requestTimeout)
	info, infoErr := client.Ping(infoContext)
	infoCancel()
	if infoErr != nil {
		return hobot.BoardUpdateCheck{}, err
	}
	fallbackContext, fallbackCancel := context.WithTimeout(base, studioUpdateTimeout)
	defer fallbackCancel()
	return checkBoardUpdateFromRelease(fallbackContext, http.DefaultClient, studioReleaseAPIURL, info.Version)
}

func isBoardUpdateNetworkFailure(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "Unable to check for Hobot Code updates within 10 seconds") || errors.Is(err, context.DeadlineExceeded)
}

// InstallBoardUpdate closes every Studio bridge before invoking the board's
// transactional updater over a separate fixed SSH command. The board remains
// responsible for task/process checks, archive verification and rollback.
func (app *App) InstallBoardUpdate(boardID string) (BoardUpdateResult, error) {
	client, err := app.client(boardID)
	if err != nil {
		return BoardUpdateResult{}, err
	}
	base := app.ctx
	if base == nil {
		base = context.Background()
	}
	preflight, cancel := context.WithTimeout(base, boardUpdateCheckTimeout)
	info, err := client.Ping(preflight)
	if err == nil && info.ActiveTasks > 0 {
		err = fmt.Errorf("%d board task(s) are active; finish or stop them before updating", info.ActiveTasks)
	}
	var check hobot.BoardUpdateCheck
	if err == nil {
		check, err = client.CheckBoardUpdate(preflight)
	}
	cancel()
	if err != nil {
		return BoardUpdateResult{}, err
	}
	if check.InstalledVersion != "" && info.Version != check.InstalledVersion {
		return BoardUpdateResult{}, fmt.Errorf("board version changed during the update check; check again")
	}
	if check.Status != "available" {
		connection, refreshErr := app.RefreshBoard(boardID)
		if refreshErr != nil {
			return BoardUpdateResult{}, refreshErr
		}
		return BoardUpdateResult{PreviousVersion: info.Version, InstalledVersion: info.Version, Message: check.Message, Connection: connection}, nil
	}

	board := app.board(boardID)
	app.disconnect(boardID)
	select {
	case <-base.Done():
		return BoardUpdateResult{}, base.Err()
	case <-time.After(500 * time.Millisecond):
	}
	updater, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		return BoardUpdateResult{}, err
	}
	updateContext, updateCancel := context.WithTimeout(base, boardUpdateTimeout)
	err = updater.InstallBoardUpdate(updateContext, check.AvailableVersion)
	updateCancel()
	_ = updater.Close()
	if err != nil {
		_, _ = app.reconnectBoardAfterUpdate(boardID, 5*time.Second)
		return BoardUpdateResult{}, err
	}

	connection, err := app.reconnectBoardAfterUpdate(boardID, boardUpdateReconnectWait)
	if err != nil {
		return BoardUpdateResult{}, fmt.Errorf("update installed but Studio could not reconnect: %w", err)
	}
	if connection.Daemon == nil || connection.Daemon.Version != check.AvailableVersion {
		actual := "unknown"
		if connection.Daemon != nil {
			actual = connection.Daemon.Version
		}
		return BoardUpdateResult{}, fmt.Errorf("updated board reported version %s, expected %s", actual, check.AvailableVersion)
	}
	return BoardUpdateResult{
		Changed: true, PreviousVersion: info.Version, InstalledVersion: connection.Daemon.Version,
		Message: "The board update was verified and Studio reconnected.", Connection: connection,
	}, nil
}

func (app *App) reconnectBoardAfterUpdate(boardID string, maximumWait time.Duration) (BoardConnection, error) {
	deadline := time.Now().Add(maximumWait)
	var lastErr error
	for {
		connection, err := app.ConnectBoard(boardID)
		if err == nil {
			return connection, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return BoardConnection{}, lastErr
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// InstallBoardService executes the remote installer over SSH on a target board.
// It prefers streaming the local Linux ARM64 release package when available,
// falling back to online download, and automatically probes the board upon completion.
func (app *App) InstallBoardService(board Board) (BoardInstallResult, error) {
	board.Name = strings.TrimSpace(board.Name)
	if board.Port == 0 {
		board.Port = 22
	}
	if board.User == "" {
		board.User = "root"
	}
	client, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		return BoardInstallResult{Success: false, Message: err.Error()}, err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var installErr error
	localArchive := findLocalLinuxArchive()
	if localArchive != "" {
		file, err := os.Open(localArchive)
		if err == nil {
			defer file.Close()
			installErr = client.InstallBoardServiceFromArchive(ctx, file)
		} else {
			installErr = err
		}
	} else {
		installErr = client.InstallBoardService(ctx, "")
	}

	if installErr != nil {
		return BoardInstallResult{
			Success: false,
			Message: fmt.Sprintf("Installation failed: %v", installErr),
		}, installErr
	}

	probe := app.ProbeBoard(board)
	if !probe.Connected {
		return BoardInstallResult{
			Success:    false,
			Message:    "Installation completed, but connecting to the board failed: " + probe.Error,
			Connection: probe,
		}, fmt.Errorf("connect after install failed: %s", probe.Error)
	}

	return BoardInstallResult{
		Success:    true,
		Message:    "Hobot Code was successfully installed and connected.",
		Connection: probe,
	}, nil
}

func findLocalLinuxArchive() string {
	candidates := []string{
		"dist/hobot-code-0.28.2-linux-arm64.tar.gz",
		"../dist/hobot-code-0.28.2-linux-arm64.tar.gz",
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "hobot-code-0.28.2-linux-arm64.tar.gz"),
			filepath.Join(exeDir, "..", "Resources", "hobot-code-0.28.2-linux-arm64.tar.gz"),
			filepath.Join(exeDir, "..", "..", "dist", "hobot-code-0.28.2-linux-arm64.tar.gz"),
		)
	}
	for _, path := range candidates {
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() && stat.Size() > 0 {
			return path
		}
	}
	for _, dir := range []string{"dist", "../dist"} {
		matches, _ := filepath.Glob(filepath.Join(dir, "hobot-code-*-linux-arm64.tar.gz"))
		if len(matches) > 0 {
			return matches[0]
		}
	}
	return ""
}

func isNotInstalledError(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "command not found") ||
		strings.Contains(lower, "hobot: not found") ||
		strings.Contains(lower, "no such file or directory") ||
		strings.Contains(lower, "closed without a response") ||
		strings.Contains(lower, "exit status 127")
}

// ProbeBoard verifies transport, protocol compatibility, and board identity
// without persisting the candidate. The structured result lets Studio present
// an actionable failure before a broken board entry reaches local storage.
func (app *App) ProbeBoard(board Board) BoardConnection {

	board.Name = strings.TrimSpace(board.Name)
	if board.Port == 0 {
		board.Port = 22
	}
	if board.User == "" {
		board.User = "root"
	}
	result := BoardConnection{Board: board}
	client, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer client.Close()
	baseContext := app.ctx
	if baseContext == nil {
		baseContext = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseContext, requestTimeout)
	defer cancel()
	info, err := client.Ping(ctx)
	if err != nil {
		errStr := err.Error()
		if isNotInstalledError(errStr) {
			result.NotInstalled = true
			result.Error = "Hobot Code is not installed on this board. You can install it automatically."
		} else {
			result.Error = errStr
		}
		return result
	}

	result.Daemon = &info
	capabilities := info.Capabilities
	result.Capabilities = &capabilities
	var snapshotErr error
	if containsValue(capabilities.Capabilities, "system.snapshot") {
		value, snapshotError := client.SystemSnapshot(ctx)
		if snapshotError != nil {
			snapshotErr = snapshotError
		} else {
			result.Snapshot = &value
		}
	}
	compatibility, compatibilityErr := assessConnectionCompatibility(info, result.Snapshot, snapshotErr)
	result.Compatibility = &compatibility
	if compatibilityErr != nil {
		result.Error = compatibilityErr.Error()
		return result
	}
	result.Connected = true
	return result
}

func (app *App) SaveBoard(board Board) (Board, error) {
	board.Name = strings.TrimSpace(board.Name)
	if board.Name == "" {
		return Board{}, fmt.Errorf("board name is required")
	}
	if board.ID == "" {
		id, err := randomID()
		if err != nil {
			return Board{}, err
		}
		board.ID = id
	}
	if !boardIDPattern.MatchString(board.ID) {
		return Board{}, fmt.Errorf("board ID is invalid")
	}
	if board.Port == 0 {
		board.Port = 22
	}
	if board.User == "" {
		board.User = "root"
	}
	probe, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		return Board{}, err
	}
	_ = probe.Close()

	app.mu.Lock()
	previous, existed := app.boards[board.ID]
	if !existed && len(app.boards) >= maximumBoards {
		app.mu.Unlock()
		return Board{}, fmt.Errorf("at most %d boards can be saved", maximumBoards)
	}
	app.boards[board.ID] = board
	if app.store == nil {
		if existed {
			app.boards[board.ID] = previous
		} else {
			delete(app.boards, board.ID)
		}
		app.mu.Unlock()
		return Board{}, fmt.Errorf("board store is unavailable")
	}
	if err := app.store.save(sortedBoards(app.boards)); err != nil {
		if existed {
			app.boards[board.ID] = previous
		} else {
			delete(app.boards, board.ID)
		}
		app.mu.Unlock()
		return Board{}, err
	}
	app.mu.Unlock()
	if existed && previous != board {
		app.disconnect(board.ID)
	}
	return board, nil
}

func (app *App) RemoveBoard(boardID string) error {
	app.mu.Lock()
	previous, ok := app.boards[boardID]
	if !ok {
		app.mu.Unlock()
		return fmt.Errorf("board does not exist")
	}
	delete(app.boards, boardID)
	if app.store == nil {
		app.boards[boardID] = previous
		app.mu.Unlock()
		return fmt.Errorf("board store is unavailable")
	}
	if err := app.store.save(sortedBoards(app.boards)); err != nil {
		app.boards[boardID] = previous
		app.mu.Unlock()
		return err
	}
	app.mu.Unlock()
	app.disconnect(boardID)
	return nil
}

func (app *App) ConnectBoard(boardID string) (BoardConnection, error) {
	app.mu.Lock()
	board, ok := app.boards[boardID]
	if !ok {
		app.mu.Unlock()
		return BoardConnection{}, fmt.Errorf("board does not exist")
	}
	app.mu.Unlock()
	app.disconnect(boardID)

	client, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		return BoardConnection{Board: board, Error: err.Error()}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	info, err := client.Ping(ctx)
	if err != nil {
		_ = client.Close()
		errStr := err.Error()
		if isNotInstalledError(errStr) {
			err = fmt.Errorf("Hobot Code is not installed on %s. Click the Edit (pencil) icon to install it with one click.", board.Name)
		}
		return BoardConnection{Board: board, Error: err.Error()}, err
	}

	capabilities := info.Capabilities
	var snapshot *hobot.SystemSnapshot
	var snapshotErr error
	if containsValue(capabilities.Capabilities, "system.snapshot") {
		value, err := client.SystemSnapshot(ctx)
		if err != nil {
			snapshotErr = err
		} else {
			snapshot = &value
		}
	}
	compatibility, compatibilityErr := assessConnectionCompatibility(info, snapshot, snapshotErr)
	if compatibilityErr != nil {
		_ = client.Close()
		return BoardConnection{Board: board, Daemon: &info, Capabilities: &capabilities, Snapshot: snapshot, Compatibility: &compatibility, Error: compatibilityErr.Error()}, compatibilityErr
	}
	app.mu.Lock()
	app.clients[boardID] = client
	app.mu.Unlock()
	return BoardConnection{Board: board, Connected: true, Daemon: &info, Capabilities: &capabilities, Snapshot: snapshot, Compatibility: &compatibility}, nil
}

func (app *App) DisconnectBoard(boardID string) {
	app.disconnect(boardID)
}

func (app *App) RefreshBoard(boardID string) (BoardConnection, error) {
	client, err := app.client(boardID)
	if err == nil {
		ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
		info, pingErr := client.Ping(ctx)
		if pingErr == nil {
			var snapshot *hobot.SystemSnapshot
			var snapshotErr error
			if containsValue(info.Capabilities.Capabilities, "system.snapshot") {
				value, err := client.SystemSnapshot(ctx)
				if err != nil {
					snapshotErr = err
				} else {
					snapshot = &value
				}
			}
			compatibility, compatibilityErr := assessConnectionCompatibility(info, snapshot, snapshotErr)
			cancel()
			if compatibilityErr == nil {
				return BoardConnection{Board: app.board(boardID), Connected: true, Daemon: &info, Capabilities: &info.Capabilities, Snapshot: snapshot, Compatibility: &compatibility}, nil
			}
			app.disconnect(boardID)
			return BoardConnection{Board: app.board(boardID), Daemon: &info, Capabilities: &info.Capabilities, Snapshot: snapshot, Compatibility: &compatibility, Error: compatibilityErr.Error()}, compatibilityErr
		}
		cancel()
		app.disconnect(boardID)
	}
	connection, connectErr := app.ConnectBoard(boardID)
	if connectErr == nil {
		connection.Reconnected = true
	}
	return connection, connectErr
}

func (app *App) GetSystemSnapshot(boardID string) (hobot.SystemSnapshot, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.SystemSnapshot{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.SystemSnapshot(ctx)
}

func (app *App) GetDiagnostics(boardID string) (hobot.DiagnosticReport, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.DiagnosticReport{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.Diagnostics(ctx)
}

func (app *App) RepairDiagnostics(boardID, action string, confirmed bool) (hobot.DiagnosticReport, error) {
	if !confirmed {
		return hobot.DiagnosticReport{}, fmt.Errorf("diagnostic repair requires explicit confirmation")
	}
	client, err := app.client(boardID)
	if err != nil {
		return hobot.DiagnosticReport{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, providerMutationTimeout)
	defer cancel()
	report, err := client.Diagnostics(ctx)
	if err != nil {
		return hobot.DiagnosticReport{}, err
	}
	var selected *hobot.DiagnosticRepairAction
	for index := range report.Repairs {
		if report.Repairs[index].ID == action {
			selected = &report.Repairs[index]
			break
		}
	}
	if selected == nil || selected.Status != "available" || !selected.RequiresConfirmation {
		return hobot.DiagnosticReport{}, fmt.Errorf("diagnostic repair is not currently available")
	}
	switch selected.Executor {
	case "agentd":
		result, err := client.RepairDiagnostics(ctx, action, true)
		if err != nil {
			return hobot.DiagnosticReport{}, err
		}
		return result.Report, nil
	case "client":
		if action != "restart-daemon" {
			return hobot.DiagnosticReport{}, fmt.Errorf("unsupported client diagnostic repair")
		}
		if err := client.RestartDaemon(ctx); err != nil {
			return hobot.DiagnosticReport{}, err
		}
		app.disconnect(boardID)
		if _, err := app.ConnectBoard(boardID); err != nil {
			return hobot.DiagnosticReport{}, fmt.Errorf("agentd restarted but Studio could not reconnect: %w", err)
		}
		return app.GetDiagnostics(boardID)
	default:
		return hobot.DiagnosticReport{}, fmt.Errorf("unsupported diagnostic repair executor")
	}
}

func (app *App) SaveSupportBundle(boardID string) (hobot.SupportBundle, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.SupportBundle{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	bundle, err := client.SupportBundle(ctx, true)
	if err != nil {
		return hobot.SupportBundle{}, err
	}
	if len(bundle.Content) == 0 || len(bundle.Content) > 4*1024*1024 {
		return hobot.SupportBundle{}, fmt.Errorf("board returned an invalid support bundle size")
	}
	digest := sha256.Sum256(bundle.Content)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), bundle.SHA256) {
		return hobot.SupportBundle{}, fmt.Errorf("support bundle integrity check failed")
	}
	filename := filepath.Base(bundle.Path)
	if filename == "." || filename == string(filepath.Separator) || !strings.HasPrefix(filename, "hobot-code-support-") || !strings.HasSuffix(filename, ".json") {
		return hobot.SupportBundle{}, fmt.Errorf("board returned an invalid support bundle name")
	}
	target, err := runtime.SaveFileDialog(app.ctx, runtime.SaveDialogOptions{
		Title: "Save Hobot Code support bundle", DefaultFilename: filename,
		CanCreateDirectories: true,
	})
	if err != nil {
		return hobot.SupportBundle{}, err
	}
	if target == "" {
		bundle.Content = nil
		bundle.Path = ""
		return bundle, nil
	}
	if err := writePrivateLocalFile(target, bundle.Content); err != nil {
		return hobot.SupportBundle{}, err
	}
	bundle.Content = nil
	bundle.Path = target
	return bundle, nil
}

func writePrivateLocalFile(path string, content []byte) error {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("support bundle destination must be absolute")
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("support bundle destination cannot be a symbolic link")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".hobot-support.*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (app *App) InspectDeployment(boardID, cwd string) (hobot.DeploymentInspection, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.DeploymentInspection{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.InspectDeployment(ctx, cwd)
}

func (app *App) StartDeployment(boardID string, request hobot.StartDeploymentRequest) (hobot.Task, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Task{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.StartDeployment(ctx, request)
}

func (app *App) GetDeploymentStatus(boardID, taskID string) (hobot.DeploymentStatus, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.DeploymentStatus{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, deploymentStatusTimeout)
	defer cancel()
	return client.DeploymentStatus(ctx, taskID)
}

func (app *App) InspectBPUModel(boardID, modelPath string) (hobot.BPUModelInfo, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.BPUModelInfo{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, 30*time.Second)
	defer cancel()
	return client.InspectBPUModel(ctx, modelPath)
}

func (app *App) RunBPUBenchmark(boardID string, req hobot.BPUBenchmarkRequest) (hobot.BPUBenchmarkResult, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.BPUBenchmarkResult{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, 2*time.Minute)
	defer cancel()
	return client.RunBPUBenchmark(ctx, req)
}

func (app *App) ListWorkspaceBPUModels(boardID, cwd string) ([]string, error) {
	client, err := app.client(boardID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.ListBPUModels(ctx, cwd)
}

func (app *App) DownloadSampleBPUModel(boardID, soc string) (string, error) {
	client, err := app.client(boardID)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(app.ctx, 2*time.Minute)
	defer cancel()
	return client.DownloadSampleBPUModel(ctx, soc)
}

func (app *App) UploadBPUModel(boardID, filename string, base64Data string) (string, error) {
	client, err := app.client(boardID)
	if err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("invalid base64 model data: %w", err)
	}
	ctx, cancel := context.WithTimeout(app.ctx, 3*time.Minute)
	defer cancel()
	return client.UploadBPUModel(ctx, filename, data)
}

func (app *App) DeleteBPUModel(boardID, modelPath string) error {
	client, err := app.client(boardID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(app.ctx, 30*time.Second)
	defer cancel()
	return client.DeleteBPUModel(ctx, modelPath)
}

func (app *App) RefreshTasks(boardID string, includeArchived bool) (hobot.TaskPage, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.TaskPage{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.Tasks(ctx, includeArchived, "", 200)
}

func (app *App) GetTask(boardID, taskID string) (hobot.Task, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Task{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.Task(ctx, taskID)
}

func (app *App) ListSchedules(boardID string, includeAll bool) ([]hobot.Schedule, error) {
	client, err := app.client(boardID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.Schedules(ctx, includeAll)
}

func (app *App) CreateSchedule(boardID string, request hobot.CreateScheduleRequest) (hobot.Schedule, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Schedule{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.CreateSchedule(ctx, request)
}

func (app *App) PauseSchedule(boardID, id string) (hobot.Schedule, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Schedule{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.PauseSchedule(ctx, id)
}

func (app *App) ResumeSchedule(boardID, id string) (hobot.Schedule, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Schedule{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.ResumeSchedule(ctx, id)
}

func (app *App) RunSchedule(boardID, id string) (hobot.Schedule, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Schedule{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.RunSchedule(ctx, id)
}

func (app *App) DeleteSchedule(boardID, id string) error {
	client, err := app.client(boardID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.DeleteSchedule(ctx, id)
}

func (app *App) GetEvents(boardID, taskID string, after uint64, limit int) (hobot.EventPage, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.EventPage{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.Events(ctx, taskID, after, limit)
}

func (app *App) GetEventsBefore(boardID, taskID string, before uint64, limit int) (hobot.EventPage, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.EventPage{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.EventsBefore(ctx, taskID, before, limit)
}

func (app *App) StartTask(boardID string, request hobot.StartTaskRequest) (hobot.Task, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Task{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, workspaceTaskTimeout)
	defer cancel()
	return client.StartTask(ctx, request)
}

func (app *App) SendPrompt(boardID, taskID, prompt string, images []hobot.ImageContent, idempotencyKey string) (hobot.PromptSubmitResult, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.PromptSubmitResult{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.SubmitPromptWithImagesAndKey(ctx, taskID, prompt, images, idempotencyKey)
}

func (app *App) ListFollowups(boardID, taskID string) (hobot.FollowupQueue, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.FollowupQueue{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.FollowupQueue(ctx, taskID)
}

func (app *App) CancelFollowup(boardID, taskID, queueID string) error {
	client, err := app.client(boardID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.CancelFollowup(ctx, taskID, queueID)
}

func (app *App) ResumeFollowups(boardID, taskID string) error {
	client, err := app.client(boardID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.ResumeFollowups(ctx, taskID)
}

func (app *App) RetryFollowup(boardID, taskID, queueID string) error {
	client, err := app.client(boardID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.RetryFollowup(ctx, taskID, queueID)
}

func (app *App) SetTaskModel(boardID, taskID, provider, modelID string) error {
	client, err := app.client(boardID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.SetModel(ctx, taskID, provider, modelID)
}

func (app *App) SetTaskPermissionMode(boardID, taskID, mode string) (hobot.Task, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Task{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.SetPermissionMode(ctx, taskID, mode)
}

func (app *App) SetTaskApprovalModel(boardID, taskID, model string) (hobot.Task, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Task{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.SetApprovalModel(ctx, taskID, model)
}

func (app *App) SetTaskSandboxMode(boardID, taskID, mode string) (hobot.Task, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Task{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.SetSandboxMode(ctx, taskID, mode)
}

func (app *App) SetTaskNetworkMode(boardID, taskID, mode string) (hobot.Task, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Task{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.SetNetworkMode(ctx, taskID, mode)
}

func (app *App) RenameTask(boardID, taskID, name string) (hobot.Task, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Task{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.RenameTask(ctx, taskID, name)
}

func (app *App) OpenExternalURL(rawURL string) error {
	target, err := safeExternalURL(rawURL)
	if err != nil {
		return err
	}
	return app.openExternalURL(target)
}

func safeExternalURL(rawURL string) (string, error) {
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") || target.User != nil {
		return "", fmt.Errorf("only HTTP and HTTPS links can be opened")
	}
	return target.String(), nil
}

func (app *App) ListModels(boardID string) ([]hobot.ModelOption, error) {
	client, err := app.client(boardID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	models, err := client.Models(ctx)
	if err != nil {
		return nil, err
	}
	return studioModels(models), nil
}

func (app *App) ListManagedProviders(boardID string) ([]hobot.ManagedProvider, error) {
	board, err := app.connectedBoard(boardID)
	if err != nil {
		return nil, err
	}
	client, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		return nil, err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.ManagedProviders(ctx)
}

func (app *App) AddManagedProvider(boardID string, request hobot.AddManagedProviderRequest, apiKey string) (ProviderMutationResult, error) {
	board, err := app.connectedBoard(boardID)
	if err != nil {
		return ProviderMutationResult{}, err
	}
	client, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		return ProviderMutationResult{}, err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(app.ctx, providerMutationTimeout)
	defer cancel()
	if err := client.AddManagedProvider(ctx, request, apiKey); err != nil {
		return ProviderMutationResult{}, err
	}
	return app.applyProviderConfiguration(ctx, boardID, client, "Provider saved")
}

func (app *App) RemoveManagedProvider(boardID, providerID string, keepCredential bool) (ProviderMutationResult, error) {
	board, err := app.connectedBoard(boardID)
	if err != nil {
		return ProviderMutationResult{}, err
	}
	client, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		return ProviderMutationResult{}, err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(app.ctx, providerMutationTimeout)
	defer cancel()
	if err := client.RemoveManagedProvider(ctx, providerID, keepCredential); err != nil {
		return ProviderMutationResult{}, err
	}
	return app.applyProviderConfiguration(ctx, boardID, client, "Provider removed")
}

func (app *App) RotateManagedProviderCredential(boardID, providerID, apiKey string, allowShared bool) (ProviderMutationResult, error) {
	board, err := app.connectedBoard(boardID)
	if err != nil {
		return ProviderMutationResult{}, err
	}
	client, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		return ProviderMutationResult{}, err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(app.ctx, providerMutationTimeout)
	defer cancel()
	if err := client.RotateManagedProviderCredential(ctx, providerID, apiKey, allowShared); err != nil {
		return ProviderMutationResult{}, err
	}
	return app.applyProviderConfiguration(ctx, boardID, client, "Provider key rotated")
}

func (app *App) ApplyProviderConfiguration(boardID string) (ProviderMutationResult, error) {
	board, err := app.connectedBoard(boardID)
	if err != nil {
		return ProviderMutationResult{}, err
	}
	client, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		return ProviderMutationResult{}, err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(app.ctx, providerMutationTimeout)
	defer cancel()
	return app.applyProviderConfiguration(ctx, boardID, client, "Provider configuration")
}

func (app *App) applyProviderConfiguration(ctx context.Context, boardID string, client *hobot.Client, prefix string) (ProviderMutationResult, error) {
	if err := client.RestartDaemon(ctx); err != nil {
		message := prefix + "; the board could not apply it yet. Retry Apply after checking the connection."
		if strings.Contains(err.Error(), "tasks_active") || strings.Contains(err.Error(), "background task(s) are active") {
			message = prefix + "; active Agent work prevented a safe restart. Apply after tasks are idle."
		}
		return ProviderMutationResult{Saved: true, Applied: false, Message: message}, nil
	}
	app.disconnect(boardID)
	if _, err := app.ConnectBoard(boardID); err != nil {
		return ProviderMutationResult{Saved: true, Applied: true, Message: prefix + " and applied. Reconnect the board to refresh Studio."}, nil
	}
	return ProviderMutationResult{Saved: true, Applied: true, Message: prefix + " and applied."}, nil
}

func (app *App) ListExtensions(boardID, taskID string) (hobot.ExtensionCatalog, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.ExtensionCatalog{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.Extensions(ctx, taskID)
}

func (app *App) CheckModelHealth(boardID, model string, force bool) (hobot.ModelHealth, error) {
	app.mu.Lock()
	board, ok := app.boards[boardID]
	app.mu.Unlock()
	if !ok {
		return hobot.ModelHealth{}, fmt.Errorf("board does not exist")
	}
	client, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		return hobot.ModelHealth{}, err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(app.ctx, modelHealthTimeout)
	defer cancel()
	return client.ModelHealth(ctx, model, force)
}

func (app *App) VerifyModel(boardID, model string, force bool) (hobot.ModelConformance, error) {
	app.mu.Lock()
	board, ok := app.boards[boardID]
	app.mu.Unlock()
	if !ok {
		return hobot.ModelConformance{}, fmt.Errorf("board does not exist")
	}
	client, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		return hobot.ModelConformance{}, err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(app.ctx, modelVerifyTimeout)
	defer cancel()
	return client.ModelConformance(ctx, model, force)
}

func (app *App) ProbeModelRuntime(boardID, model string) (hobot.ModelRuntimeProbe, error) {
	app.mu.Lock()
	board, ok := app.boards[boardID]
	app.mu.Unlock()
	if !ok {
		return hobot.ModelRuntimeProbe{}, fmt.Errorf("board does not exist")
	}
	client, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		return hobot.ModelRuntimeProbe{}, err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(app.ctx, modelRuntimeTimeout)
	defer cancel()
	return client.ModelRuntimeProbe(ctx, model)
}

func (app *App) ProbeModelRDK(boardID, model, profile string) (hobot.ModelRDKProbe, error) {
	app.mu.Lock()
	board, ok := app.boards[boardID]
	app.mu.Unlock()
	if !ok {
		return hobot.ModelRDKProbe{}, fmt.Errorf("board does not exist")
	}
	client, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		return hobot.ModelRDKProbe{}, err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(app.ctx, modelRDKTimeout)
	defer cancel()
	return client.ModelRDKProbe(ctx, model, profile)
}

func (app *App) GetModelRDKMatrix(boardID, model string) (hobot.ModelRDKMatrix, error) {
	app.mu.Lock()
	board, ok := app.boards[boardID]
	app.mu.Unlock()
	if !ok {
		return hobot.ModelRDKMatrix{}, fmt.Errorf("board does not exist")
	}
	client, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		return hobot.ModelRDKMatrix{}, err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(app.ctx, modelQualificationTimeout)
	defer cancel()
	return client.ModelRDKMatrix(ctx, model)
}

func (app *App) GetModelQualification(boardID, model string) (hobot.ModelQualification, error) {
	app.mu.Lock()
	board, ok := app.boards[boardID]
	app.mu.Unlock()
	if !ok {
		return hobot.ModelQualification{}, fmt.Errorf("board does not exist")
	}
	client, err := hobot.NewClient(boardConfig(board))
	if err != nil {
		return hobot.ModelQualification{}, err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(app.ctx, modelQualificationTimeout)
	defer cancel()
	return client.ModelQualification(ctx, model)
}

func studioModels(models []hobot.ModelOption) []hobot.ModelOption {
	allowed := map[string]int{
		"kimi-k3":                    0,
		"kimi-k2.6":                  1,
		"kimi@latest":                2,
		"qwen3.8-max":                3,
		"qwen3.7-max":                4,
		"qwen-max@latest":            5,
		"glm-5.2":                    6,
		"glm-5.3":                    7,
		"glm@latest":                 8,
		"deepseek/deepseek-v4-flash": 9,
		"deepseek-v4-flash":          10,
		"deepseek-v4-pro":            11,
		"deepseek-flash@latest":      12,
		"deepseek-pro@latest":        13,
	}
	filtered := make([]hobot.ModelOption, 0, len(models))
	for _, model := range models {
		if model.Provider == "drobotics" {
			if _, ok := allowed[model.ID]; !ok {
				continue
			}
			filtered = append(filtered, model)
		} else if model.Managed {
			filtered = append(filtered, model)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		left, leftBuiltIn := allowed[filtered[i].ID]
		right, rightBuiltIn := allowed[filtered[j].ID]
		if leftBuiltIn != rightBuiltIn {
			return leftBuiltIn
		}
		if leftBuiltIn {
			return left < right
		}
		if filtered[i].Provider != filtered[j].Provider {
			return filtered[i].Provider < filtered[j].Provider
		}
		return filtered[i].ID < filtered[j].ID
	})
	return filtered
}

func (app *App) BrowseWorkspace(boardID, path string) (hobot.WorkspaceListing, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.WorkspaceListing{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.BrowseWorkspace(ctx, path)
}

func (app *App) CreateWorkspace(boardID, parent, name string) (hobot.WorkspaceListing, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.WorkspaceListing{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.CreateWorkspace(ctx, parent, name)
}

func (app *App) GetWorkspaceChanges(boardID, taskID string) (hobot.WorkspaceChanges, error) {
	if !taskIDPattern.MatchString(taskID) {
		return hobot.WorkspaceChanges{}, fmt.Errorf("task id is invalid")
	}
	client, err := app.client(boardID)
	if err != nil {
		return hobot.WorkspaceChanges{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.WorkspaceChanges(ctx, taskID)
}

func (app *App) InspectWorkspaceIsolation(boardID, path string) (hobot.WorkspaceIsolation, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.WorkspaceIsolation{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.InspectWorkspaceIsolation(ctx, path)
}

func (app *App) InspectWorkspaceDelivery(boardID, taskID string) (hobot.WorkspaceDelivery, error) {
	if !taskIDPattern.MatchString(taskID) {
		return hobot.WorkspaceDelivery{}, fmt.Errorf("task id is invalid")
	}
	client, err := app.client(boardID)
	if err != nil {
		return hobot.WorkspaceDelivery{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, workspaceTaskTimeout)
	defer cancel()
	return client.InspectWorkspaceDelivery(ctx, taskID)
}

func (app *App) ApplyWorkspace(boardID, taskID, expectedDigest string) (hobot.WorkspaceApplyResult, error) {
	if !taskIDPattern.MatchString(taskID) {
		return hobot.WorkspaceApplyResult{}, fmt.Errorf("task id is invalid")
	}
	if len(expectedDigest) != sha256.Size*2 {
		return hobot.WorkspaceApplyResult{}, fmt.Errorf("reviewed workspace digest is invalid")
	}
	if _, err := hex.DecodeString(expectedDigest); err != nil || strings.ToLower(expectedDigest) != expectedDigest {
		return hobot.WorkspaceApplyResult{}, fmt.Errorf("reviewed workspace digest is invalid")
	}
	client, err := app.client(boardID)
	if err != nil {
		return hobot.WorkspaceApplyResult{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, workspaceTaskTimeout)
	defer cancel()
	return client.ApplyWorkspace(ctx, taskID, expectedDigest)
}

func (app *App) ForkTask(boardID string, request hobot.ForkTaskRequest) (hobot.Task, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Task{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.ForkTask(ctx, request)
}

func (app *App) StopTask(boardID, taskID string) error {
	client, err := app.client(boardID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.StopTask(ctx, taskID)
}

func (app *App) AbortTask(boardID, taskID string) error {
	client, err := app.client(boardID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.Abort(ctx, taskID)
}

func (app *App) DeleteTasks(boardID string, taskIDs []string) error {
	if len(taskIDs) == 0 || len(taskIDs) > 200 {
		return fmt.Errorf("delete between 1 and 200 conversations at a time")
	}
	client, err := app.client(boardID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(app.ctx, deleteTimeout)
	defer cancel()
	seen := make(map[string]bool, len(taskIDs))
	tasks := make([]hobot.Task, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		if seen[taskID] || !taskIDPattern.MatchString(taskID) {
			return fmt.Errorf("conversation ID is invalid or duplicated")
		}
		seen[taskID] = true
		current, err := client.Task(ctx, taskID)
		if err != nil {
			return err
		}
		tasks = append(tasks, current)
	}
	for index, current := range tasks {
		if studioTaskIsLive(current.Status) {
			if current.Status != "stopping" {
				if err := client.StopTask(ctx, current.ID); err != nil {
					return err
				}
			}
			for studioTaskIsLive(current.Status) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(50 * time.Millisecond):
				}
				var err error
				current, err = client.Task(ctx, current.ID)
				if err != nil {
					return err
				}
			}
		}
		tasks[index] = current
	}
	for _, current := range tasks {
		if _, err := client.ArchiveTask(ctx, current.ID, true); err != nil {
			return err
		}
		if err := client.DeleteTask(ctx, current.ID); err != nil {
			return err
		}
	}
	return nil
}

func studioTaskIsLive(status string) bool {
	switch status {
	case "queued", "starting", "idle", "running", "waiting", "stopping":
		return true
	default:
		return false
	}
}

func (app *App) ResumeTask(boardID, taskID, prompt string, images []hobot.ImageContent) (hobot.Task, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Task{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.ResumeTaskWithImages(ctx, taskID, prompt, images)
}

func (app *App) RestartTask(boardID, taskID, prompt string, images []hobot.ImageContent) (hobot.Task, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Task{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.RestartTaskWithImages(ctx, taskID, prompt, images)
}

func (app *App) RespondApproval(boardID, taskID, approvalID string, response map[string]any) error {
	client, err := app.client(boardID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.Respond(ctx, taskID, approvalID, response)
}

func (app *App) WatchTask(boardID, taskID string, after uint64) error {
	client, err := app.client(boardID)
	if err != nil {
		return err
	}
	key := boardID + ":" + taskID
	app.mu.Lock()
	if watcher, ok := app.watchers[key]; ok {
		watcher.cancel()
	}
	ctx, cancel := context.WithCancel(app.ctx)
	app.nextWatcher++
	watcherID := app.nextWatcher
	app.watchers[key] = taskWatcher{id: watcherID, cancel: cancel}
	app.mu.Unlock()
	go func() {
		nextAfter := after
		backoff := time.Second
		attempt := 0
		for {
			connectedAt := time.Time{}
			err := client.SubscribeWithState(ctx, taskID, nextAfter, func(state hobot.SubscriptionState) {
				connectedAt = time.Now()
				runtime.EventsEmit(app.ctx, "task:watch-status", TaskWatchStatus{
					BoardID: boardID, TaskID: taskID, State: "connected",
					RetainedFrom: state.RetainedFrom, RetainedThrough: state.RetainedThrough,
					LatestSequence: state.LatestSequence, HistoryTruncated: state.HistoryTruncated,
					CursorExpired: state.CursorExpired,
				})
			}, func(event hobot.Event) error {
				attempt = 0
				backoff = time.Second
				nextAfter = event.Sequence
				runtime.EventsEmit(app.ctx, "task:event", TaskEventEnvelope{BoardID: boardID, Event: event})
				return nil
			})
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				break
			}
			if err == nil {
				break
			}
			if !connectedAt.IsZero() && time.Since(connectedAt) >= 30*time.Second {
				attempt = 0
				backoff = time.Second
			}
			if !hobot.IsTransientSubscriptionError(err) {
				runtime.EventsEmit(app.ctx, "task:watch-status", TaskWatchStatus{
					BoardID: boardID, TaskID: taskID, State: "failed", Message: err.Error(),
				})
				runtime.EventsEmit(app.ctx, "task:watch-error", map[string]string{
					"boardId": boardID, "taskId": taskID, "error": "Event stream failed: " + err.Error(),
				})
				break
			}
			attempt++
			runtime.EventsEmit(app.ctx, "task:watch-status", TaskWatchStatus{
				BoardID: boardID, TaskID: taskID, State: "reconnecting", Attempt: attempt, Message: err.Error(),
			})
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			case <-timer.C:
			}
			if ctx.Err() != nil {
				break
			}
			if backoff < 15*time.Second {
				backoff *= 2
				if backoff > 15*time.Second {
					backoff = 15 * time.Second
				}
			}
		}
		app.mu.Lock()
		if watcher, ok := app.watchers[key]; ok && watcher.id == watcherID {
			delete(app.watchers, key)
		}
		app.mu.Unlock()
	}()
	return nil
}

func (app *App) StopWatchingTask(boardID, taskID string) {
	app.mu.Lock()
	defer app.mu.Unlock()
	key := boardID + ":" + taskID
	if watcher, ok := app.watchers[key]; ok {
		watcher.cancel()
		delete(app.watchers, key)
	}
}

func (app *App) client(boardID string) (*hobot.Client, error) {
	app.mu.Lock()
	defer app.mu.Unlock()
	client := app.clients[boardID]
	if client == nil {
		return nil, fmt.Errorf("board is not connected")
	}
	return client, nil
}

func (app *App) connectedBoard(boardID string) (Board, error) {
	app.mu.Lock()
	defer app.mu.Unlock()
	board, exists := app.boards[boardID]
	if !exists || app.clients[boardID] == nil {
		return Board{}, fmt.Errorf("board is not connected")
	}
	return board, nil
}

func (app *App) board(boardID string) Board {
	app.mu.Lock()
	defer app.mu.Unlock()
	return app.boards[boardID]
}

func (app *App) disconnect(boardID string) {
	app.mu.Lock()
	var cancellations []context.CancelFunc
	for key, watcher := range app.watchers {
		if len(key) > len(boardID) && key[:len(boardID)+1] == boardID+":" {
			cancellations = append(cancellations, watcher.cancel)
			delete(app.watchers, key)
		}
	}
	client := app.clients[boardID]
	delete(app.clients, boardID)
	app.mu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
	if client != nil {
		_ = client.Close()
	}
}

func boardConfig(board Board) hobot.Config {
	return hobot.Config{
		Host: board.Host, User: board.User, Port: board.Port, IdentityFile: board.IdentityFile,
		HostKeyPolicy: "accept-new",
	}
}

func sortedBoards(values map[string]Board) []Board {
	boards := make([]Board, 0, len(values))
	for _, board := range values {
		boards = append(boards, board)
	}
	sort.Slice(boards, func(i, j int) bool { return boards[i].Name < boards[j].Name })
	return boards
}

func randomID() (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

type boardStore struct{ path string }

func newBoardStore() (*boardStore, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "Hobot Code")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	return &boardStore{path: filepath.Join(dir, "boards.json")}, nil
}

func (store *boardStore) load() ([]Board, error) {
	info, err := os.Lstat(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("board store must be a regular file")
	}
	if info.Size() > maximumBoardFileSize {
		return nil, fmt.Errorf("board store exceeds %d bytes", maximumBoardFileSize)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("board store permissions must be 0600")
	}
	content, err := os.ReadFile(store.path)
	if err != nil {
		return nil, err
	}
	var boards []Board
	if err := json.Unmarshal(content, &boards); err != nil {
		return nil, err
	}
	if len(boards) > maximumBoards {
		return nil, fmt.Errorf("board store contains more than %d boards", maximumBoards)
	}
	seen := make(map[string]struct{}, len(boards))
	for _, board := range boards {
		if !boardIDPattern.MatchString(board.ID) || strings.TrimSpace(board.Name) == "" {
			return nil, fmt.Errorf("board store contains an invalid board")
		}
		if _, exists := seen[board.ID]; exists {
			return nil, fmt.Errorf("board store contains duplicate board IDs")
		}
		seen[board.ID] = struct{}{}
	}
	return boards, nil
}

func (store *boardStore) save(boards []Board) error {
	content, err := json.MarshalIndent(boards, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(store.path), ".boards.*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(content, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, store.path)
}
