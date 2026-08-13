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
	ProtocolMin          int               `json:"protocolMin"`
	ProtocolMax          int               `json:"protocolMax"`
	EventSchema          int               `json:"eventSchema"`
	Capabilities         []string          `json:"capabilities"`
	MaximumRequestBytes  int               `json:"maximumRequestBytes"`
	MaximumResponseBytes int               `json:"maximumResponseBytes"`
	MaximumPromptBytes   int               `json:"maximumPromptBytes"`
	MaximumActiveTasks   int               `json:"maximumActiveTasks"`
	MaximumRetainedTasks int               `json:"maximumRetainedTasks"`
	Sandbox              SandboxCapability `json:"sandbox"`
}

type SandboxCapability struct {
	Available        bool     `json:"available"`
	Backend          string   `json:"backend,omitempty"`
	Profiles         []string `json:"profiles,omitempty"`
	FilesystemWrites bool     `json:"filesystemWritesRestricted"`
	Devices          bool     `json:"devicesRestricted"`
	Capabilities     bool     `json:"capabilitiesDropped"`
	Network          bool     `json:"networkRestricted"`
	Reason           string   `json:"reason,omitempty"`
}

type ExtensionCatalog struct {
	SchemaVersion  int                   `json:"schemaVersion"`
	APIVersion     string                `json:"apiVersion"`
	ProductVersion string                `json:"productVersion"`
	HostVersion    string                `json:"hostVersion"`
	CapturedAt     string                `json:"capturedAt,omitempty"`
	Entries        []ExtensionEntry      `json:"entries"`
	Diagnostics    []ExtensionDiagnostic `json:"diagnostics,omitempty"`
	Policy         ExtensionPolicy       `json:"policy"`
}

type ExtensionDiagnostic struct {
	Source  string `json:"source"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type ExtensionEntry struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	Kind           string   `json:"kind"`
	Description    string   `json:"description"`
	Origin         string   `json:"origin"`
	Scope          string   `json:"scope"`
	Runtime        string   `json:"runtime"`
	Entrypoint     string   `json:"entrypoint"`
	Trust          string   `json:"trust"`
	DefaultEnabled bool     `json:"defaultEnabled"`
	Required       bool     `json:"required"`
	Provides       []string `json:"provides"`
	Requires       []string `json:"requires"`
	Permissions    []string `json:"permissions"`
	Targets        []string `json:"targets"`
	Status         string   `json:"status,omitempty"`
	StatusDetail   string   `json:"statusDetail,omitempty"`
}

type ExtensionPolicy struct {
	InventoryOnly       bool   `json:"inventoryOnly"`
	ExecutionAuthority  string `json:"executionAuthority"`
	PermissionAuthority string `json:"permissionAuthority"`
	ThirdPartyRuntime   string `json:"thirdPartyRuntime"`
	HotReload           bool   `json:"hotReload"`
}

type DaemonInfo struct {
	Version              string        `json:"version"`
	Protocol             int           `json:"protocol"`
	PID                  int           `json:"pid"`
	StartedAt            time.Time     `json:"startedAt"`
	ActiveTasks          int           `json:"activeTasks"`
	QueuedTasks          int           `json:"queuedTasks"`
	MaximumTasks         int           `json:"maximumTasks"`
	SocketPath           string        `json:"socketPath"`
	StateRoot            string        `json:"stateRoot"`
	BackgroundTasks      bool          `json:"backgroundTasks"`
	ConfigurationCurrent *bool         `json:"configurationCurrent,omitempty"`
	Capabilities         Capabilities  `json:"capabilities"`
	Build                BuildIdentity `json:"build"`
}

type BuildIdentity struct {
	Status       string     `json:"status"`
	Reason       string     `json:"reason,omitempty"`
	Commit       string     `json:"commit,omitempty"`
	Dirty        *bool      `json:"dirty,omitempty"`
	BuiltAt      *time.Time `json:"builtAt,omitempty"`
	Target       string     `json:"target,omitempty"`
	BinarySHA256 string     `json:"binarySha256,omitempty"`
	PiVersion    string     `json:"piVersion,omitempty"`
	PiCommit     string     `json:"piCommit,omitempty"`
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

type BPUTelemetryInfo struct {
	Status string `json:"status"`
	Source string `json:"source,omitempty"`
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

type AcceleratorMemoryPoolInfo struct {
	Name         string `json:"name"`
	TotalBytes   uint64 `json:"totalBytes"`
	UsedBytes    uint64 `json:"usedBytes"`
	FreeBytes    uint64 `json:"freeBytes"`
	ProcessBytes uint64 `json:"processBytes,omitempty"`
	SystemBytes  uint64 `json:"systemBytes,omitempty"`
}

type AcceleratorProcessInfo struct {
	PID        int    `json:"pid"`
	Name       string `json:"name"`
	RSSBytes   uint64 `json:"rssBytes"`
	HbmemBytes uint64 `json:"hbmemBytes"`
}

type AcceleratorInfo struct {
	Available     bool                        `json:"available"`
	Source        string                      `json:"source,omitempty"`
	CapturedAt    time.Time                   `json:"capturedAt,omitempty"`
	DDRReadMiBPS  float64                     `json:"ddrReadMiBps,omitempty"`
	DDRWriteMiBPS float64                     `json:"ddrWriteMiBps,omitempty"`
	HbmemPools    []AcceleratorMemoryPoolInfo `json:"hbmemPools,omitempty"`
	Processes     []AcceleratorProcessInfo    `json:"processes,omitempty"`
}

type HardwareLease struct {
	Resource   string    `json:"resource"`
	TaskID     string    `json:"taskId"`
	PID        int       `json:"pid"`
	Cwd        string    `json:"cwd,omitempty"`
	AcquiredAt time.Time `json:"acquiredAt"`
}

type WorkspaceWriteLease struct {
	TaskID     string    `json:"taskId"`
	PID        int       `json:"pid"`
	Cwd        string    `json:"cwd"`
	AcquiredAt time.Time `json:"acquiredAt"`
}

type WorkspaceWriteLeaseList struct {
	Leases []WorkspaceWriteLease `json:"leases"`
}

type DiskInfo struct {
	Path           string `json:"path"`
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}

type SystemSnapshot struct {
	CapturedAt      time.Time             `json:"capturedAt"`
	Board           string                `json:"board"`
	BoardID         string                `json:"boardId"`
	Hostname        string                `json:"hostname"`
	RDKOSVersion    string                `json:"rdkOsVersion"`
	Kernel          string                `json:"kernel"`
	Architecture    string                `json:"architecture"`
	CPUCores        int                   `json:"cpuCores"`
	LoadAverage     []float64             `json:"loadAverage"`
	Memory          MemoryInfo            `json:"memory"`
	Disk            DiskInfo              `json:"disk"`
	ThermalZones    []ThermalZone         `json:"thermalZones"`
	BPUDevices      []string              `json:"bpuDevices"`
	BPUCores        []BPUCoreInfo         `json:"bpuCores"`
	BPUTelemetry    BPUTelemetryInfo      `json:"bpuTelemetry"`
	AIMemory        AIMemoryInfo          `json:"aiMemory"`
	Accelerator     AcceleratorInfo       `json:"accelerator"`
	HardwareLeases  []HardwareLease       `json:"hardwareLeases,omitempty"`
	WorkspaceWrites []WorkspaceWriteLease `json:"workspaceWrites,omitempty"`
	RDKUtilities    map[string]bool       `json:"rdkUtilities"`
	UptimeSeconds   uint64                `json:"uptimeSeconds"`
}

type SupportBundle struct {
	ID        string              `json:"id"`
	CreatedAt time.Time           `json:"createdAt"`
	Path      string              `json:"path"`
	SizeBytes int                 `json:"sizeBytes"`
	SHA256    string              `json:"sha256"`
	Content   []byte              `json:"content,omitempty"`
	Excluded  []string            `json:"excluded"`
	Checks    SupportCheckSummary `json:"checks"`
}

type SupportCheckSummary struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
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
	Schema     int                  `json:"schema"`
	Cwd        string               `json:"cwd"`
	Board      string               `json:"board"`
	BoardID    string               `json:"boardId"`
	RDKOS      string               `json:"rdkOsVersion"`
	Goal       string               `json:"goal"`
	Artifact   DeploymentArtifact   `json:"artifact"`
	ReportPath string               `json:"reportPath"`
	CreatedAt  time.Time            `json:"createdAt"`
	Acceptance DeploymentAcceptance `json:"acceptance,omitempty"`
}

type DeploymentMetricRequirement struct {
	Name       string  `json:"name"`
	Unit       string  `json:"unit"`
	Threshold  float64 `json:"threshold"`
	Comparator string  `json:"comparator"`
}
type DeploymentAcceptance struct {
	Profile                     string                        `json:"profile"`
	Dataset                     string                        `json:"dataset,omitempty"`
	MinimumAccuracySamples      int                           `json:"minimumAccuracySamples"`
	Metrics                     []DeploymentMetricRequirement `json:"metrics,omitempty"`
	MinimumWarmupIterations     int                           `json:"minimumWarmupIterations"`
	MinimumMeasuredIterations   int                           `json:"minimumMeasuredIterations"`
	MaximumModelP95LatencyMS    float64                       `json:"maximumModelP95LatencyMs,omitempty"`
	MaximumEndToEndP95LatencyMS float64                       `json:"maximumEndToEndP95LatencyMs,omitempty"`
	MinimumThroughput           float64                       `json:"minimumThroughput"`
	MaximumTemperatureC         float64                       `json:"maximumTemperatureC"`
	MinimumMemoryAvailableBytes uint64                        `json:"minimumMemoryAvailableBytes"`
}

type DeploymentReport struct {
	Schema         int    `json:"schema"`
	Outcome        string `json:"outcome"`
	BoardID        string `json:"boardId"`
	ArtifactPath   string `json:"artifactPath"`
	ArtifactSHA256 string `json:"artifactSha256,omitempty"`
	Summary        string `json:"summary"`
	Correctness    struct {
		Passed            bool               `json:"passed"`
		Method            string             `json:"method,omitempty"`
		Dataset           string             `json:"dataset,omitempty"`
		SampleCount       int                `json:"sampleCount,omitempty"`
		ReferenceArtifact string             `json:"referenceArtifact,omitempty"`
		Metrics           []DeploymentMetric `json:"metrics,omitempty"`
	} `json:"correctness"`
	Performance struct {
		WarmupIterations int     `json:"warmupIterations,omitempty"`
		Iterations       int     `json:"iterations,omitempty"`
		P50LatencyMS     float64 `json:"p50LatencyMs,omitempty"`
		P95LatencyMS     float64 `json:"p95LatencyMs,omitempty"`
		Throughput       float64 `json:"throughput,omitempty"`
		EndToEndP50MS    float64 `json:"endToEndP50Ms,omitempty"`
		EndToEndP95MS    float64 `json:"endToEndP95Ms,omitempty"`
	} `json:"performance"`
	Resources DeploymentResources `json:"resources,omitempty"`
}

type DeploymentMetric struct {
	Name       string  `json:"name"`
	Unit       string  `json:"unit"`
	Value      float64 `json:"value"`
	Threshold  float64 `json:"threshold"`
	Comparator string  `json:"comparator"`
	Passed     bool    `json:"passed"`
}

type DeploymentResourceSample struct {
	CapturedAt                 time.Time `json:"capturedAt"`
	AIAllocationAvailable      bool      `json:"aiAllocationAvailable"`
	BPUUtilizationAvailable    bool      `json:"bpuUtilizationAvailable"`
	TemperatureAvailable       bool      `json:"temperatureAvailable"`
	SystemMemoryUsedBytes      uint64    `json:"systemMemoryUsedBytes,omitempty"`
	SystemMemoryAvailableBytes uint64    `json:"systemMemoryAvailableBytes,omitempty"`
	AIAllocationSource         string    `json:"aiAllocationSource,omitempty"`
	AIAllocatedBytes           uint64    `json:"aiAllocatedBytes,omitempty"`
	IONAllocatedBytes          uint64    `json:"ionAllocatedBytes,omitempty"`
	BPUUtilizationPercent      float64   `json:"bpuUtilizationPercent,omitempty"`
	CPULoadPercent             float64   `json:"cpuLoadPercent,omitempty"`
	MaxTemperatureC            float64   `json:"maxTemperatureC,omitempty"`
}

type DeploymentResources struct {
	SampleCount int                      `json:"sampleCount,omitempty"`
	Baseline    DeploymentResourceSample `json:"baseline,omitempty"`
	Peak        DeploymentResourceSample `json:"peak,omitempty"`
	Final       DeploymentResourceSample `json:"final,omitempty"`
	Limits      struct {
		MaxTemperatureC               float64 `json:"maxTemperatureC,omitempty"`
		MinSystemMemoryAvailableBytes uint64  `json:"minSystemMemoryAvailableBytes,omitempty"`
	} `json:"limits,omitempty"`
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
	SandboxMode    string `json:"sandboxMode,omitempty"`
	Profile        string `json:"profile,omitempty"`
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
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	Cwd              string             `json:"cwd"`
	ProjectCwd       string             `json:"projectCwd,omitempty"`
	WorkspaceMode    string             `json:"workspaceMode"`
	WorkspaceID      string             `json:"workspaceId,omitempty"`
	WorktreePath     string             `json:"worktreePath,omitempty"`
	WorktreeBase     string             `json:"worktreeBase,omitempty"`
	Status           string             `json:"status"`
	PID              int                `json:"pid,omitempty"`
	CreatedAt        time.Time          `json:"createdAt"`
	UpdatedAt        time.Time          `json:"updatedAt"`
	LastSequence     uint64             `json:"lastSequence"`
	LogTruncated     bool               `json:"logTruncated,omitempty"`
	LastError        string             `json:"lastError,omitempty"`
	Failure          *TaskFailure       `json:"failure,omitempty"`
	SessionFile      string             `json:"sessionFile,omitempty"`
	SessionID        string             `json:"sessionId,omitempty"`
	Approved         bool               `json:"approved,omitempty"`
	ResumeCount      int                `json:"resumeCount,omitempty"`
	RestartCount     int                `json:"restartCount,omitempty"`
	Model            string             `json:"model,omitempty"`
	PermissionMode   string             `json:"permissionMode,omitempty"`
	SandboxMode      string             `json:"sandboxMode"`
	Sandbox          TaskSandboxStatus  `json:"sandbox"`
	ParentTaskID     string             `json:"parentTaskId,omitempty"`
	ForkSequence     uint64             `json:"forkSequence,omitempty"`
	BranchKind       string             `json:"branchKind,omitempty"`
	AwaitingPrompt   bool               `json:"awaitingPrompt,omitempty"`
	QueuedAt         *time.Time         `json:"queuedAt,omitempty"`
	QueueOperation   string             `json:"queueOperation,omitempty"`
	ArchivedAt       *time.Time         `json:"archivedAt,omitempty"`
	PendingApprovals []Approval         `json:"pendingApprovals,omitempty"`
	Deployment       *DeploymentRecord  `json:"deployment,omitempty"`
	TurnEvidence     []TaskTurnEvidence `json:"turnEvidence,omitempty"`
}

type TurnWorkspaceEvidence struct {
	Status       string    `json:"status"`
	CapturedAt   time.Time `json:"capturedAt"`
	StateDigest  string    `json:"stateDigest,omitempty"`
	Dirty        bool      `json:"dirty,omitempty"`
	ChangedFiles int       `json:"changedFiles,omitempty"`
	Truncated    bool      `json:"truncated,omitempty"`
}

type TaskTurnEvidence struct {
	Turn              uint64                 `json:"turn"`
	Status            string                 `json:"status"`
	Evidence          string                 `json:"evidence"`
	StartedAt         time.Time              `json:"startedAt"`
	EndedAt           *time.Time             `json:"endedAt,omitempty"`
	StartSequence     uint64                 `json:"startSequence"`
	EndSequence       uint64                 `json:"endSequence,omitempty"`
	ToolsStarted      int                    `json:"toolsStarted"`
	ToolsCompleted    int                    `json:"toolsCompleted"`
	ToolsFailed       int                    `json:"toolsFailed"`
	OpenTools         int                    `json:"openTools"`
	WorkspaceBefore   *TurnWorkspaceEvidence `json:"workspaceBefore,omitempty"`
	WorkspaceAfter    *TurnWorkspaceEvidence `json:"workspaceAfter,omitempty"`
	WorkspaceChanged  *bool                  `json:"workspaceChanged,omitempty"`
	RecommendedAction string                 `json:"recommendedAction"`
}

type TaskSandboxStatus struct {
	Requested            string `json:"requested"`
	Effective            string `json:"effective"`
	Backend              string `json:"backend"`
	FilesystemRestricted bool   `json:"filesystemRestricted"`
	DevicesRestricted    bool   `json:"devicesRestricted"`
	CapabilitiesDropped  bool   `json:"capabilitiesDropped"`
	NetworkRestricted    bool   `json:"networkRestricted"`
	Reason               string `json:"reason,omitempty"`
}

type TaskFailure struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Recovery string `json:"recovery"`
}

type NormalizedEvent struct {
	Schema int             `json:"schema"`
	Type   string          `json:"type"`
	Data   map[string]any  `json:"data,omitempty"`
	Item   *NormalizedItem `json:"item,omitempty"`
}

type NormalizedItem struct {
	ID     string `json:"id,omitempty"`
	Type   string `json:"type"`
	Status string `json:"status"`
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
	Prompt         string         `json:"prompt,omitempty"`
	Images         []ImageContent `json:"images,omitempty"`
	Approve        bool           `json:"approve,omitempty"`
	Model          string         `json:"model,omitempty"`
	PermissionMode string         `json:"permissionMode,omitempty"`
	WorkspaceMode  string         `json:"workspaceMode,omitempty"`
	SandboxMode    string         `json:"sandboxMode,omitempty"`
}

type ForkTaskRequest struct {
	TaskID         string         `json:"taskId"`
	Sequence       uint64         `json:"sequence,omitempty"`
	Prompt         string         `json:"prompt,omitempty"`
	Images         []ImageContent `json:"images,omitempty"`
	Name           string         `json:"name,omitempty"`
	Kind           string         `json:"kind,omitempty"`
	Model          string         `json:"model,omitempty"`
	PermissionMode string         `json:"permissionMode,omitempty"`
	SandboxMode    string         `json:"sandboxMode,omitempty"`
}

type ImageContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
	Name     string `json:"name,omitempty"`
}

type ModelOption struct {
	Provider         string            `json:"provider"`
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Default          bool              `json:"default,omitempty"`
	Capabilities     ModelCapabilities `json:"capabilities"`
	CapabilitySource string            `json:"capabilitySource"`
}

type ModelCapabilities struct {
	Reasoning  bool `json:"reasoning"`
	ImageInput bool `json:"imageInput"`
}

type ModelHealth struct {
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	Status      string    `json:"status"`
	Category    string    `json:"category"`
	Message     string    `json:"message"`
	Transport   string    `json:"transport,omitempty"`
	CheckedAt   time.Time `json:"checkedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	FirstByteMS int64     `json:"firstByteMs,omitempty"`
	LatencyMS   int64     `json:"latencyMs,omitempty"`
	Attempts    int       `json:"attempts"`
	Cached      bool      `json:"cached"`
}

type ModelConformanceCheck struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Category  string `json:"category"`
	Message   string `json:"message"`
	LatencyMS int64  `json:"latencyMs,omitempty"`
}

type ModelConformance struct {
	Provider   string                  `json:"provider"`
	Model      string                  `json:"model"`
	Status     string                  `json:"status"`
	Message    string                  `json:"message"`
	CheckedAt  time.Time               `json:"checkedAt"`
	ExpiresAt  time.Time               `json:"expiresAt"`
	DurationMS int64                   `json:"durationMs,omitempty"`
	Attempts   int                     `json:"attempts"`
	Cached     bool                    `json:"cached"`
	Checks     []ModelConformanceCheck `json:"checks"`
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

type WorkspaceChangeFile struct {
	Path         string `json:"path"`
	OriginalPath string `json:"originalPath,omitempty"`
	Status       string `json:"status"`
	Kind         string `json:"kind"`
	Staged       bool   `json:"staged,omitempty"`
	Unstaged     bool   `json:"unstaged,omitempty"`
	Untracked    bool   `json:"untracked,omitempty"`
	Conflict     bool   `json:"conflict,omitempty"`
}

type WorkspaceChanges struct {
	CapturedAt     time.Time             `json:"capturedAt"`
	Available      bool                  `json:"available"`
	Repository     bool                  `json:"repository"`
	RepositoryRoot string                `json:"repositoryRoot,omitempty"`
	Scope          string                `json:"scope,omitempty"`
	Head           string                `json:"head,omitempty"`
	Files          []WorkspaceChangeFile `json:"files"`
	Patch          string                `json:"patch,omitempty"`
	FilesTruncated bool                  `json:"filesTruncated,omitempty"`
	PatchTruncated bool                  `json:"patchTruncated,omitempty"`
}

type WorkspaceIsolation struct {
	CapturedAt      time.Time `json:"capturedAt"`
	Available       bool      `json:"available"`
	Repository      bool      `json:"repository"`
	Eligible        bool      `json:"eligible"`
	RecommendedMode string    `json:"recommendedMode"`
	RepositoryRoot  string    `json:"repositoryRoot,omitempty"`
	Scope           string    `json:"scope,omitempty"`
	Head            string    `json:"head,omitempty"`
	Clean           bool      `json:"clean"`
	Reason          string    `json:"reason"`
}

type ManagedWorktree struct {
	TaskID       string    `json:"taskId"`
	ProjectCwd   string    `json:"projectCwd"`
	Path         string    `json:"path"`
	BaseRevision string    `json:"baseRevision"`
	CreatedAt    time.Time `json:"createdAt"`
	InUse        bool      `json:"inUse"`
}

type ManagedWorktreeList struct {
	Worktrees []ManagedWorktree `json:"worktrees"`
	Truncated bool              `json:"truncated,omitempty"`
}

type WorkspaceCleanupResult struct {
	TaskID  string `json:"taskId"`
	Cleaned bool   `json:"cleaned"`
}

type WorkspaceDelivery struct {
	TaskID         string `json:"taskId"`
	Ready          bool   `json:"ready"`
	Reason         string `json:"reason"`
	PatchBytes     int    `json:"patchBytes,omitempty"`
	Digest         string `json:"digest,omitempty"`
	AlreadyApplied bool   `json:"alreadyApplied,omitempty"`
}

type WorkspaceApplyResult struct {
	TaskID     string    `json:"taskId"`
	Applied    bool      `json:"applied"`
	Staged     bool      `json:"staged"`
	PatchBytes int       `json:"patchBytes"`
	Digest     string    `json:"digest"`
	AppliedAt  time.Time `json:"appliedAt"`
}
