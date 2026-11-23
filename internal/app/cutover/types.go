package cutover

// Input defines parameters for the cutover use-case.
type Input struct {
	UnitID string `json:"unit_id"`
	Action string `json:"action"` // "plan", "status", "kill"
	Dir    string `json:"dir"`
}

// Output encapsulates cutover status, planning advisories, and kill switch outcomes.
// Rule R-1: NEVER performs lift or exposes lift operations.
type Output struct {
	SchemaVersion    string   `json:"schema_version,omitempty"`
	UnitID           string   `json:"unit_id"`
	Action           string   `json:"action"`
	RouteState       string   `json:"route_state"`
	KillSwitchActive bool     `json:"kill_switch_active"`
	TargetService    string   `json:"target_service,omitempty"`
	FallbackCount    int64    `json:"fallback_count"`
	Fallbacks        int64    `json:"fallbacks"`
	Warnings         []string `json:"warnings,omitempty"`
	Message          string   `json:"message,omitempty"`
}
