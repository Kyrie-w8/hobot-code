package main

import "time"

type taskCLISummary struct {
	ID                    string                  `json:"id"`
	Name                  string                  `json:"name"`
	Status                taskStatus              `json:"status"`
	Cwd                   string                  `json:"cwd"`
	ProjectCwd            string                  `json:"projectCwd,omitempty"`
	WorkspaceMode         string                  `json:"workspaceMode"`
	WorkspaceID           string                  `json:"workspaceId,omitempty"`
	WorktreeBase          string                  `json:"worktreeBase,omitempty"`
	CreatedAt             time.Time               `json:"createdAt"`
	UpdatedAt             time.Time               `json:"updatedAt"`
	LastSequence          uint64                  `json:"lastSequence"`
	Model                 string                  `json:"model,omitempty"`
	PermissionMode        string                  `json:"permissionMode,omitempty"`
	SandboxMode           string                  `json:"sandboxMode"`
	NetworkMode           string                  `json:"networkMode"`
	Sandbox               taskSandboxStatus       `json:"sandbox"`
	Approved              bool                    `json:"projectResourcesTrusted"`
	ParentTaskID          string                  `json:"parentTaskId,omitempty"`
	SourceTaskID          string                  `json:"sourceTaskId,omitempty"`
	BranchKind            string                  `json:"branchKind,omitempty"`
	CurrentActivity       string                  `json:"currentActivity,omitempty"`
	QueuedAt              *time.Time              `json:"queuedAt,omitempty"`
	QueueOperation        string                  `json:"queueOperation,omitempty"`
	ArchivedAt            *time.Time              `json:"archivedAt,omitempty"`
	Failure               *taskFailure            `json:"failure,omitempty"`
	PendingApprovalCount  int                     `json:"pendingApprovalCount"`
	InactiveApprovalCount int                     `json:"inactiveApprovalCount,omitempty"`
	Deployment            *deploymentCLISummary   `json:"deployment,omitempty"`
	LastTurnEvidence      *turnEvidenceCLISummary `json:"lastTurnEvidence,omitempty"`
}

type turnEvidenceCLISummary struct {
	Turn              uint64 `json:"turn"`
	Status            string `json:"status"`
	Evidence          string `json:"evidence"`
	ToolsStarted      int    `json:"toolsStarted"`
	ToolsCompleted    int    `json:"toolsCompleted"`
	ToolsFailed       int    `json:"toolsFailed"`
	OpenTools         int    `json:"openTools"`
	WorkspaceStatus   string `json:"workspaceStatus,omitempty"`
	WorkspaceChanged  *bool  `json:"workspaceChanged,omitempty"`
	RecommendedAction string `json:"recommendedAction"`
}

type deploymentCLISummary struct {
	Schema            int       `json:"schema"`
	Board             string    `json:"board"`
	RDKOS             string    `json:"rdkOsVersion"`
	Goal              string    `json:"goal"`
	ArtifactName      string    `json:"artifactName"`
	ArtifactKind      string    `json:"artifactKind"`
	Compatibility     string    `json:"compatibility"`
	AcceptanceProfile string    `json:"acceptanceProfile,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
}

type approvalCLISummary struct {
	ID          string    `json:"id"`
	Method      string    `json:"method"`
	Title       string    `json:"title"`
	RequestedAt time.Time `json:"requestedAt"`
	Active      bool      `json:"active"`
}

func summarizeTaskForCLI(metadata taskMetadata) taskCLISummary {
	summary := taskCLISummary{
		ID: metadata.ID, Name: metadata.Name, Status: metadata.Status, Cwd: metadata.Cwd,
		ProjectCwd: metadata.ProjectCwd, WorkspaceMode: metadata.WorkspaceMode, WorkspaceID: metadata.WorkspaceID,
		WorktreeBase: metadata.WorktreeBase, CreatedAt: metadata.CreatedAt, UpdatedAt: metadata.UpdatedAt,
		LastSequence: metadata.LastSequence, Model: metadata.Model, PermissionMode: metadata.PermissionMode,
		SandboxMode: metadata.SandboxMode, NetworkMode: metadata.NetworkMode, Sandbox: metadata.Sandbox,
		Approved: metadata.Approved, ParentTaskID: metadata.ParentTaskID, SourceTaskID: metadata.SourceTaskID,
		BranchKind: metadata.BranchKind, CurrentActivity: metadata.CurrentActivity,
		QueuedAt: metadata.QueuedAt, QueueOperation: metadata.QueueOperation, ArchivedAt: metadata.ArchivedAt,
		Failure: metadata.Failure,
	}
	if metadata.Deployment != nil {
		summary.Deployment = &deploymentCLISummary{
			Schema: metadata.Deployment.Schema, Board: metadata.Deployment.Board, RDKOS: metadata.Deployment.RDKOS,
			Goal: metadata.Deployment.Goal, ArtifactName: metadata.Deployment.Artifact.Name,
			ArtifactKind: metadata.Deployment.Artifact.Kind, Compatibility: metadata.Deployment.Artifact.Compatibility,
			AcceptanceProfile: metadata.Deployment.Acceptance.Profile, CreatedAt: metadata.Deployment.CreatedAt,
		}
	}
	if len(metadata.TurnEvidence) > 0 {
		last := metadata.TurnEvidence[len(metadata.TurnEvidence)-1]
		summary.LastTurnEvidence = &turnEvidenceCLISummary{
			Turn: last.Turn, Status: last.Status, Evidence: last.Evidence, ToolsStarted: last.ToolsStarted,
			ToolsCompleted: last.ToolsCompleted, ToolsFailed: last.ToolsFailed, OpenTools: last.OpenTools,
			WorkspaceChanged: last.WorkspaceChanged, RecommendedAction: last.RecommendedAction,
		}
		if last.WorkspaceAfter != nil {
			summary.LastTurnEvidence.WorkspaceStatus = last.WorkspaceAfter.Status
		} else if last.WorkspaceBefore != nil {
			summary.LastTurnEvidence.WorkspaceStatus = last.WorkspaceBefore.Status
		}
	}
	for _, approval := range metadata.Approvals {
		if approval.Active {
			summary.PendingApprovalCount++
		} else {
			summary.InactiveApprovalCount++
		}
	}
	return summary
}

func summarizeApprovalsForCLI(approvals []pendingApproval) []approvalCLISummary {
	result := make([]approvalCLISummary, 0, len(approvals))
	for _, approval := range approvals {
		result = append(result, approvalCLISummary{
			ID: approval.ID, Method: approval.Method, Title: safeAttachText(approval.Title, "Approval required", 120),
			RequestedAt: approval.RequestedAt, Active: approval.Active,
		})
	}
	return result
}
