package setup

// ToolName represents supported AI agent coding tools.
type ToolName string

const (
	ToolClaudeCode ToolName = "claude-code"
	ToolCodex      ToolName = "codex"
	ToolCursor     ToolName = "cursor"
	ToolGemini     ToolName = "gemini"
	ToolOpenCode   ToolName = "opencode"
	ToolAll        ToolName = "all"
)

// Scope specifies installation scope: project or global user level.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeUser    Scope = "user"
)

// Options holds parameters for agent setup and configuration management.
type Options struct {
	Tool        string `json:"tool"`
	Scope       string `json:"scope"`
	Dir         string `json:"dir"`
	DryRun      bool   `json:"dry_run"`
	Yes         bool   `json:"yes"`
	Uninstall   bool   `json:"uninstall"`
	BinaryPath  string `json:"binary_path,omitempty"`
	ProjectName string `json:"project_name,omitempty"`
}

// FileChange describes an edit, creation, or deletion on an agent config file.
type FileChange struct {
	Path        string `json:"path"`
	Action      string `json:"action"` // "create", "modify", "remove", "unchanged"
	BackupPath  string `json:"backup_path,omitempty"`
	Diff        string `json:"diff,omitempty"`
	Instruction string `json:"instruction_file,omitempty"`
}

// Result encapsulates the summary report of setup execution.
type Result struct {
	Tool      string       `json:"tool"`
	Scope     string       `json:"scope"`
	DryRun    bool         `json:"dry_run"`
	Uninstall bool         `json:"uninstall"`
	Changes   []FileChange `json:"changes"`
	Message   string       `json:"message"`
}
