package hobot

import (
	"encoding/json"
	"time"
)

const ProtocolVersion = 1

type Config struct {
	Host           string
	User           string
	Port           int
	IdentityFile   string
	ConnectTimeout time.Duration
	HostKeyPolicy  string
	SSHBinary      string
}

type ProtocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (err *ProtocolError) Error() string {
	if err == nil {
		return ""
	}
	return err.Code + ": " + err.Message
}

type responseEnvelope struct {
	Protocol int             `json:"protocol"`
	ID       string          `json:"id"`
	OK       bool            `json:"ok"`
	Result   json.RawMessage `json:"result"`
	Error    *ProtocolError  `json:"error"`
}

type Capabilities struct {
	ProtocolMin          int      `json:"protocolMin"`
	ProtocolMax          int      `json:"protocolMax"`
	EventSchema          int      `json:"eventSchema"`
	Capabilities         []string `json:"capabilities"`
	MaximumRequestBytes  int      `json:"maximumRequestBytes"`
	MaximumResponseBytes int      `json:"maximumResponseBytes"`
	MaximumPromptBytes   int      `json:"maximumPromptBytes"`
	MaximumActiveTasks   int      `json:"maximumActiveTasks"`
	MaximumRetainedTasks int      `json:"maximumRetainedTasks"`
}

type DaemonInfo struct {
	Version         string       `json:"version"`
	Protocol        int          `json:"protocol"`
	PID             int          `json:"pid"`
	StartedAt       time.Time    `json:"startedAt"`
	ActiveTasks     int          `json:"activeTasks"`
	MaximumTasks    int          `json:"maximumTasks"`
	SocketPath      string       `json:"socketPath"`
	StateRoot       string       `json:"stateRoot"`
	BackgroundTasks bool         `json:"backgroundTasks"`
	Capabilities    Capabilities `json:"capabilities"`
}

type ThermalZone struct {
	Name    string  `json:"name"`
	Celsius float64 `json:"celsius"`
}

type MemoryInfo struct {
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}

type DiskInfo struct {
	Path           string `json:"path"`
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}

type SystemSnapshot struct {
	CapturedAt    time.Time       `json:"capturedAt"`
	Board         string          `json:"board"`
	BoardID       string          `json:"boardId"`
	Hostname      string          `json:"hostname"`
	RDKOSVersion  string          `json:"rdkOsVersion"`
	Kernel        string          `json:"kernel"`
	Architecture  string          `json:"architecture"`
	CPUCores      int             `json:"cpuCores"`
	LoadAverage   []float64       `json:"loadAverage"`
	Memory        MemoryInfo      `json:"memory"`
	Disk          DiskInfo        `json:"disk"`
	ThermalZones  []ThermalZone   `json:"thermalZones"`
	BPUDevices    []string        `json:"bpuDevices"`
	RDKUtilities  map[string]bool `json:"rdkUtilities"`
	UptimeSeconds uint64          `json:"uptimeSeconds"`
}

type Approval struct {
	ID          string    `json:"id"`
	Method      string    `json:"method"`
	Title       string    `json:"title,omitempty"`
	Message     string    `json:"message,omitempty"`
	Options     []string  `json:"options,omitempty"`
	Placeholder string    `json:"placeholder,omitempty"`
	Prefill     string    `json:"prefill,omitempty"`
	TimeoutMS   int       `json:"timeoutMs,omitempty"`
	RequestedAt time.Time `json:"requestedAt"`
	Active      bool      `json:"active"`
}

type Task struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Cwd              string     `json:"cwd"`
	Status           string     `json:"status"`
	PID              int        `json:"pid,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	LastSequence     uint64     `json:"lastSequence"`
	LogTruncated     bool       `json:"logTruncated,omitempty"`
	LastError        string     `json:"lastError,omitempty"`
	SessionFile      string     `json:"sessionFile,omitempty"`
	SessionID        string     `json:"sessionId,omitempty"`
	Approved         bool       `json:"approved,omitempty"`
	ResumeCount      int        `json:"resumeCount,omitempty"`
	RestartCount     int        `json:"restartCount,omitempty"`
	Model            string     `json:"model,omitempty"`
	PermissionMode   string     `json:"permissionMode,omitempty"`
	ParentTaskID     string     `json:"parentTaskId,omitempty"`
	ForkSequence     uint64     `json:"forkSequence,omitempty"`
	BranchKind       string     `json:"branchKind,omitempty"`
	ArchivedAt       *time.Time `json:"archivedAt,omitempty"`
	PendingApprovals []Approval `json:"pendingApprovals,omitempty"`
}

type NormalizedEvent struct {
	Schema int            `json:"schema"`
	Type   string         `json:"type"`
	Data   map[string]any `json:"data,omitempty"`
}

type Event struct {
	Protocol   int              `json:"protocol"`
	Kind       string           `json:"kind"`
	TaskID     string           `json:"taskId"`
	Sequence   uint64           `json:"sequence"`
	Time       time.Time        `json:"time"`
	Raw        json.RawMessage  `json:"event"`
	Normalized *NormalizedEvent `json:"normalized,omitempty"`
}

type TaskPage struct {
	Tasks      []Task `json:"tasks"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type EventPage struct {
	Events    []Event `json:"events"`
	NextAfter uint64  `json:"nextAfter,omitempty"`
	HasMore   bool    `json:"hasMore"`
}

type StartTaskRequest struct {
	Name           string         `json:"name,omitempty"`
	Cwd            string         `json:"cwd"`
	Prompt         string         `json:"prompt"`
	Images         []ImageContent `json:"images,omitempty"`
	Approve        bool           `json:"approve,omitempty"`
	Model          string         `json:"model,omitempty"`
	PermissionMode string         `json:"permissionMode,omitempty"`
}

type ForkTaskRequest struct {
	TaskID         string         `json:"taskId"`
	Sequence       uint64         `json:"sequence,omitempty"`
	Prompt         string         `json:"prompt"`
	Images         []ImageContent `json:"images,omitempty"`
	Name           string         `json:"name,omitempty"`
	Kind           string         `json:"kind,omitempty"`
	Model          string         `json:"model,omitempty"`
	PermissionMode string         `json:"permissionMode,omitempty"`
}

type ImageContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
	Name     string `json:"name,omitempty"`
}

type ModelOption struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Name     string `json:"name"`
}

type WorkspaceEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type WorkspaceListing struct {
	Path        string           `json:"path"`
	Parent      string           `json:"parent,omitempty"`
	Home        string           `json:"home"`
	Directories []WorkspaceEntry `json:"directories"`
}
