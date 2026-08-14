package main

type rdkProbeProfileDefinition struct {
	ID            string
	Name          string
	Workflow      string
	EvidenceClass string
	Description   string
	Query         string
	Topic         string
	Runnable      bool
	Targets       []string
	NotCovered    []string
}

var rdkProbeProfiles = []rdkProbeProfileDefinition{
	{
		ID: rdkProbeProfile, Name: "Board diagnostics", Workflow: "board-diagnostics", EvidenceClass: "live-read-only",
		Description: "Live board identity and version-matched diagnostic knowledge with strict evidence synthesis.",
		Query:       rdkProbeKnowledgeQuery, Topic: "diagnostics", Runnable: true, Targets: []string{"x5", "s100", "s600"},
		NotCovered: []string{"workspace-coding", "model-deployment", "multimedia-pipeline", "hardware-control"},
	},
	{
		ID: "read-only-model-deployment-planning-v1", Name: "Model deployment planning", Workflow: "model-deployment", EvidenceClass: "knowledge-grounded-planning",
		Description: "Board-aware model conversion, quantization, inference, and validation planning against versioned official knowledge.",
		Query:       "bpu model quantization inference deployment validation", Topic: "deployment", Runnable: true, Targets: []string{"x5", "s100", "s600"},
		NotCovered: []string{"model-conversion", "board-inference", "accuracy-validation", "performance-benchmark"},
	},
	{
		ID: "read-only-multimedia-planning-v1", Name: "Multimedia pipeline planning", Workflow: "multimedia-pipeline", EvidenceClass: "knowledge-grounded-planning",
		Description: "Board-aware camera, codec, display, and TROS pipeline planning against versioned official knowledge.",
		Query:       "camera codec display multimedia tros pipeline", Topic: "multimedia", Runnable: true, Targets: []string{"x5", "s100", "s600"},
		NotCovered: []string{"camera-capture", "codec-execution", "pipeline-throughput", "device-integration"},
	},
	{
		ID: "read-only-hardware-safety-planning-v1", Name: "Hardware safety planning", Workflow: "hardware-safety", EvidenceClass: "knowledge-grounded-planning",
		Description: "Board-aware power, boot, permission, rollback, and control-risk planning without hardware mutation.",
		Query:       "hardware power boot permissions rollback safety control", Topic: "safety", Runnable: true, Targets: []string{"x5", "s100", "s600"},
		NotCovered: []string{"gpio-write", "can-control", "firmware-update", "power-cycle"},
	},
	{
		ID: "isolated-workspace-coding-v1", Name: "Workspace coding", Workflow: "workspace-coding", EvidenceClass: "not-implemented",
		Description: "Isolated repository inspection, bounded edit, verification, and change reporting.",
		Runnable:    false, Targets: []string{"x5", "s100", "s600"},
		NotCovered: []string{"repository-edit", "quality-gate", "change-review"},
	},
}

func rdkProbeProfileByID(id string) (rdkProbeProfileDefinition, bool) {
	if id == "" {
		id = rdkProbeProfile
	}
	for _, profile := range rdkProbeProfiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return rdkProbeProfileDefinition{}, false
}

func defaultRDKProbeProfile() rdkProbeProfileDefinition {
	profile, _ := rdkProbeProfileByID(rdkProbeProfile)
	return profile
}
