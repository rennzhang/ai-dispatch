package contract

type DispatchRequest struct {
	Command                string                       `json:"command"`
	Target                 string                       `json:"target,omitempty"`
	Prompt                 string                       `json:"prompt,omitempty"`
	PromptFile             string                       `json:"prompt_file,omitempty"`
	OutputFile             string                       `json:"output_file,omitempty"`
	Model                  string                       `json:"model,omitempty"`
	CWD                    string                       `json:"cwd,omitempty"`
	SessionID              string                       `json:"session_id,omitempty"`
	SessionProvider        string                       `json:"session_provider,omitempty"`
	JSONResult             bool                         `json:"json_result"`
	StreamProgress         bool                         `json:"stream_progress"`
	TimeoutSeconds         int                          `json:"timeout_seconds"`
	ActivityTimeoutSeconds int                          `json:"activity_timeout_seconds"`
	TaskName               string                       `json:"task_name,omitempty"`
	CallerEnv              string                       `json:"caller_env,omitempty"`
	CallerProvider         string                       `json:"caller_provider,omitempty"`
	CallerModule           string                       `json:"caller_module,omitempty"`
	NoMetaHeader           bool                         `json:"no_meta_header,omitempty"`
	ProviderOpts           map[string]map[string]string `json:"provider_opts,omitempty"`
}
