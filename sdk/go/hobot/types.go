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

type BPUCoreInfo struct {
	Index              int     `json:"index"`
	Name               string  `json:"name"`
	UtilizationPercent float64 `json:"utilizationPercent"`
	CurrentFrequencyHz uint64  `json:"currentFrequencyHz,omitempty"`
	MinimumFrequencyHz uint64  `json:"minimumFrequencyHz,omitempty"`
	MaximumFrequencyHz uint64  `json:"maximumFrequencyHz,omitempty"`
}

type AIMemoryHeapInfo struct {
	Name           string `json:"name"`
	CapacityBytes  uint64 `json:"capacityBytes,omitempty"`
	AllocatedBytes uint64 `json:"allocatedBytes"`
	OrphanedBytes  uint64 `json:"orphanedBytes,omitempty"`
}

type AIMemoryInfo struct {
	Available              bool               `json:"available"`
	BPUAllocationAvailable bool               `json:"bpuAllocationAvailable"`
	IONAvailable           bool               `json:"ionAvailable"`
	CMAAvailable           bool               `json:"cmaAvailable"`
	DMABufAvailable        bool               `json:"dmaBufAvailable"`
	BPUAllocatedBytes      uint64             `json:"bpuAllocatedBytes,omitempty"`
	IONAllocatedBytes      uint64             `json:"ionAllocatedBytes,omitempty"`
	IONOrphanedBytes       uint64             `json:"ionOrphanedBytes,omitempty"`
	CMATotalBytes          uint64             `json:"cmaTotalBytes,omitempty"`
	CMAFreeBytes           uint64             `json:"cmaFreeBytes,omitempty"`
	DMABufBytes            uint64             `json:"dmaBufBytes,omitempty"`
	DMABufObjects          uint64             `json:"dmaBufObjects,omitempty"`
	Heaps                  []AIMemoryHeapInfo `json:"heaps,omitempty"`
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
	BPUCores      []BPUCoreInfo   `json:"bpuCores"`
	AIMemory      AIMemoryInfo    `json:"aiMemory"`
	RDKUtilities  map[string]bool `json:"rdkUtilities"`
	UptimeSeconds uint64          `json:"uptimeSeconds"`
}

type DeploymentArtifact struct {
	Path          string    `json:"path"`
	RelativePath  string    `json:"relativePath"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	SizeBytes     int64     `json:"sizeBytes"`
	ModifiedAt    time.Time `json:"modifiedAt"`
	Compatibility string    `json:"compatibility"`
	Reason        string    `json:"reason"`
}

type DeploymentInspection struct {
	CapturedAt time.Time            `json:"capturedAt"`
	Cwd        string               `json:"cwd"`
	Board      string               `json:"board"`
	BoardID    string               `json:"boardId"`
	RDKOS      string               `json:"rdkOsVersion"`
	Artifacts  []DeploymentArtifact `json:"artifacts"`
	Truncated  bool                 `json:"truncated"`
}

type DeploymentRecord struct {
	Schema     int                `json:"schema"`
	Cwd        string             `json:"cwd"`
	Board      string             `json:"board"`
	BoardID    string             `json:"boardId"`
	RDKOS      string             `json:"rdkOsVersion"`
	Goal       string             `json:"goal"`
	Artifact   DeploymentArtifact `json:"artifact"`
	ReportPath string             `json:"reportPath"`
	CreatedAt  time.Time          `json:"createdAt"`
}

type DeploymentReport struct {
	Schema         int    `json:"schema"`
	Outcome        string `json:"outcome"`
	BoardID        string `json:"boardId"`
	ArtifactPath   string `json:"artifactPath"`
	ArtifactSHA256 string `json:"artifactSha256,omitempty"`
	Summary        string `json:"summary"`
	Correctness    struct {
		Passed bool   `json:"passed"`
		Method string `json:"method,omitempty"`
	} `json:"correctness"`
	Performance struct {
		WarmupIterations int     `json:"warmupIterations,omitempty"`
		Iterations       int     `json:"iterations,omitempty"`
		P50LatencyMS     float64 `json:"p50LatencyMs,omitempty"`
		P95LatencyMS     float64 `json:"p95LatencyMs,omitempty"`
		Throughput       float64 `json:"throughput,omitempty"`
	} `json:"performance"`
}

type DeploymentStatus struct {
	TaskID     string            `json:"taskId"`
	Phase      string            `json:"phase"`
	Deployment DeploymentRecord  `json:"deployment"`
	Report     *DeploymentReport `json:"report,omitempty"`
	Issue      string            `json:"issue,omitempty"`
}

type StartDeploymentRequest struct {
	Cwd            string `json:"cwd"`
	ArtifactPath   string `json:"artifactPath"`
	Goal           string `json:"goal,omitempty"`
	Name           string `json:"name,omitempty"`
	Model          string `json:"model,omitempty"`
	PermissionMode string `json:"permissionMode,omitempty"`
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
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Cwd              string            `json:"cwd"`
	Status           string            `json:"status"`
	PID              int               `json:"pid,omitempty"`
	CreatedAt        time.Time         `json:"createdAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
	LastSequence     uint64            `json:"lastSequence"`
	LogTruncated     bool              `json:"logTruncated,omitempty"`
	LastError        string            `json:"lastError,omitempty"`
	SessionFile      string            `json:"sessionFile,omitempty"`
	SessionID        string            `json:"sessionId,omitempty"`
	Approved         bool              `json:"approved,omitempty"`
	ResumeCount      int               `json:"resumeCount,omitempty"`
	RestartCount     int               `json:"restartCount,omitempty"`
	Model            string            `json:"model,omitempty"`
	PermissionMode   string            `json:"permissionMode,omitempty"`
	ParentTaskID     string            `json:"parentTaskId,omitempty"`
	ForkSequence     uint64            `json:"forkSequence,omitempty"`
	BranchKind       string            `json:"branchKind,omitempty"`
	ArchivedAt       *time.Time        `json:"archivedAt,omitempty"`
	PendingApprovals []Approval        `json:"pendingApprovals,omitempty"`
	Deployment       *DeploymentRecord `json:"deployment,omitempty"`
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
