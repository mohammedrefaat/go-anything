package hotpath

// StepStatus represents the outcome status of an individual pipeline step.
type StepStatus string

const (
	StepStatusPending    StepStatus = "pending"
	StepStatusInProgress StepStatus = "in_progress"
	StepStatusCompleted  StepStatus = "completed"
	StepStatusSkipped    StepStatus = "skipped"
	StepStatusWaiting    StepStatus = "waiting"
	StepStatusFailed     StepStatus = "failed"
)

// PipelineStatus indicates the overall state of the guided hot-path pipeline.
type PipelineStatus string

const (
	PipelineStatusInProgress              PipelineStatus = "in_progress"
	PipelineStatusPausedForImplementation PipelineStatus = "paused_for_implementation"
	PipelineStatusReadyForApproval        PipelineStatus = "ready_for_approval"
	PipelineStatusCompleted               PipelineStatus = "completed"
	PipelineStatusFailed                  PipelineStatus = "failed"
)

// Pipeline step identifiers.
const (
	StepTarget     = "target"
	StepRecord     = "record"
	StepBridge     = "bridge"
	StepImplement  = "implement"
	StepReplayDiff = "replay_diff"
	StepFuzzBench  = "fuzz_bench"
	StepGate       = "gate"
	StepShadow     = "shadow"
)

// Input specifies configuration parameters for the hot-path porting pipeline.
type Input struct {
	UnitID   string `json:"unit_id"`
	Function string `json:"function,omitempty"`
	Dir      string `json:"dir,omitempty"`
	Step     string `json:"step,omitempty"`
	Force    bool   `json:"force,omitempty"`
}

// StepResult represents the status and output summary of a pipeline stage.
type StepResult struct {
	Name      string     `json:"name"`
	Status    StepStatus `json:"status"`
	Summary   string     `json:"summary,omitempty"`
	Artifacts []string   `json:"artifacts,omitempty"`
}

// Output encapsulates the pipeline execution report.
type Output struct {
	SchemaVersion       string         `json:"schema_version,omitempty"`
	UnitID              string         `json:"unit_id"`
	Function            string         `json:"function"`
	UnitState           string         `json:"unit_state"`
	CurrentStep         string         `json:"current_step"`
	Status              PipelineStatus `json:"status"`
	Steps               []StepResult   `json:"steps"`
	HumanActionRequired string         `json:"human_action_required,omitempty"`
	Timestamp           string         `json:"timestamp"`
}
