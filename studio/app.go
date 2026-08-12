package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	requestTimeout       = 20 * time.Second
	deleteTimeout        = 45 * time.Second
	maximumBoards        = 64
	maximumBoardFileSize = 1024 * 1024
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
	Board        Board               `json:"board"`
	Connected    bool                `json:"connected"`
	Daemon       *hobot.DaemonInfo   `json:"daemon,omitempty"`
	Capabilities *hobot.Capabilities `json:"capabilities,omitempty"`
	Error        string              `json:"error,omitempty"`
}

type TaskEventEnvelope struct {
	BoardID string      `json:"boardId"`
	Event   hobot.Event `json:"event"`
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
		return BoardConnection{Board: board, Error: err.Error()}, err
	}
	capabilities := info.Capabilities
	if capabilities.EventSchema < 2 {
		_ = client.Close()
		return BoardConnection{Board: board}, fmt.Errorf("board requires a newer Hobot Code event schema")
	}
	app.mu.Lock()
	app.clients[boardID] = client
	app.mu.Unlock()
	return BoardConnection{Board: board, Connected: true, Daemon: &info, Capabilities: &capabilities}, nil
}

func (app *App) DisconnectBoard(boardID string) {
	app.disconnect(boardID)
}

func (app *App) RefreshBoard(boardID string) (BoardConnection, error) {
	client, err := app.client(boardID)
	if err == nil {
		ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
		info, pingErr := client.Ping(ctx)
		cancel()
		if pingErr == nil {
			return BoardConnection{Board: app.board(boardID), Connected: true, Daemon: &info, Capabilities: &info.Capabilities}, nil
		}
		app.disconnect(boardID)
	}
	return app.ConnectBoard(boardID)
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

func (app *App) GetEvents(boardID, taskID string, after uint64, limit int) (hobot.EventPage, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.EventPage{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.Events(ctx, taskID, after, limit)
}

func (app *App) StartTask(boardID string, request hobot.StartTaskRequest) (hobot.Task, error) {
	client, err := app.client(boardID)
	if err != nil {
		return hobot.Task{}, err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.StartTask(ctx, request)
}

func (app *App) SendPrompt(boardID, taskID, prompt string, images []hobot.ImageContent) error {
	client, err := app.client(boardID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(app.ctx, requestTimeout)
	defer cancel()
	return client.SendPromptWithImages(ctx, taskID, prompt, images)
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
	if app.ctx == nil {
		return fmt.Errorf("application is not ready")
	}
	runtime.BrowserOpenURL(app.ctx, target)
	return nil
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

func studioModels(models []hobot.ModelOption) []hobot.ModelOption {
	allowed := map[string]int{"kimi-k3": 0, "qwen3.8-max": 1, "glm-5.2": 2}
	filtered := make([]hobot.ModelOption, 0, len(models))
	for _, model := range models {
		if model.Provider == "drobotics" {
			if _, ok := allowed[model.ID]; !ok {
				continue
			}
			filtered = append(filtered, model)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return allowed[filtered[i].ID] < allowed[filtered[j].ID] })
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
	case "starting", "idle", "running", "waiting", "stopping":
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
		for {
			err := client.Subscribe(ctx, taskID, nextAfter, func(event hobot.Event) error {
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
			runtime.EventsEmit(app.ctx, "task:watch-error", map[string]string{
				"boardId": boardID, "taskId": taskID, "error": "Event stream reconnecting: " + err.Error(),
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
