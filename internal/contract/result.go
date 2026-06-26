package contract

type Status string

const (
	StatusSuccess  Status = "success"
	StatusQuota    Status = "quota"
	StatusTimeout  Status = "timeout"
	StatusNotFound Status = "not_found"
	StatusDisabled Status = "disabled"
	StatusError    Status = "error"
)

type FailureClass string

const (
	FailureNetwork FailureClass = "network"
	FailureQuota   FailureClass = "quota"
	FailureTimeout FailureClass = "timeout"
	FailureConfig  FailureClass = "config"
	FailureRuntime FailureClass = "runtime"
	FailureInput   FailureClass = "input"
	FailureUnknown FailureClass = "unknown"
)

type NextAction string

const (
	NextDone           NextAction = "done"
	NextRetry          NextAction = "retry"
	NextSwitchStrategy NextAction = "switch_strategy"
	NextSwitchAccount  NextAction = "switch_account"
	NextFixInput       NextAction = "fix_input"
	NextInspectError   NextAction = "inspect_error"
)

type UsageInfo struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheReadTokens     int `json:"cache_read_tokens"`
	CacheCreationTokens int `json:"cache_creation_tokens"`
}

type CostInfo struct {
	Amount float64 `json:"amount"`
	Source string  `json:"source"`
}

type RouteStep struct {
	Provider   string `json:"provider"`
	Model      string `json:"model,omitempty"`
	Status     Status `json:"status"`
	Reason     string `json:"reason,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type ProviderResult struct {
	SchemaVersion   string        `json:"schema_version"`
	OK              bool          `json:"ok"`
	Status          Status        `json:"status"`
	Text            string        `json:"text,omitempty"`
	ProviderUsed    string        `json:"provider_used"`
	ModelUsed       string        `json:"model_used,omitempty"`
	SessionID       string        `json:"session_id,omitempty"`
	RequestedTarget string        `json:"requested_target,omitempty"`
	RouteTrace      []string      `json:"route_trace"`
	RouteSteps      []RouteStep   `json:"route_steps"`
	Degraded        bool          `json:"degraded"`
	DegradeReason   string        `json:"degrade_reason,omitempty"`
	Usage           *UsageInfo    `json:"usage,omitempty"`
	Cost            *CostInfo     `json:"cost,omitempty"`
	ExitCode        int           `json:"exit_code"`
	DurationMS      int64         `json:"duration_ms"`
	Stderr          string        `json:"stderr"`
	Warnings        []string      `json:"warnings"`
	OutputFile      string        `json:"output_file,omitempty"`
	NextAction      NextAction    `json:"next_action"`
	FailureClass    *FailureClass `json:"failure_class"`
}

func SuccessResult(text string) ProviderResult {
	return ProviderResult{
		SchemaVersion: "2.0",
		OK:            true,
		Status:        StatusSuccess,
		Text:          text,
		RouteTrace:    []string{},
		RouteSteps:    []RouteStep{},
		Warnings:      []string{},
		ExitCode:      0,
		Stderr:        "",
		NextAction:    NextDone,
	}
}

func ErrorResult(status Status, failure FailureClass, message string, exitCode int) ProviderResult {
	return ProviderResult{
		SchemaVersion: "2.0",
		OK:            false,
		Status:        status,
		RouteTrace:    []string{},
		RouteSteps:    []RouteStep{},
		Warnings:      []string{},
		ExitCode:      exitCode,
		Stderr:        message,
		NextAction:    NextActionForFailure(failure, ""),
		FailureClass:  &failure,
	}
}

func NextActionForFailure(failure FailureClass, provider string) NextAction {
	switch failure {
	case FailureInput:
		return NextFixInput
	case FailureTimeout, FailureRuntime:
		return NextRetry
	case FailureQuota:
		if provider == "codex" {
			return NextSwitchAccount
		}
		return NextSwitchStrategy
	case FailureNetwork, FailureConfig:
		return NextSwitchStrategy
	default:
		return NextInspectError
	}
}
