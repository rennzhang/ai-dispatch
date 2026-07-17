package contract

import (
	"fmt"
	"strings"
)

// Effort is the cross-provider reasoning effort level requested by the caller.
// auto means ai-dispatch does not override the provider/CLI default.
type Effort string

const (
	EffortAuto    Effort = "auto"
	EffortNone    Effort = "none"
	EffortMinimal Effort = "minimal"
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
	EffortXHigh   Effort = "xhigh"
	EffortMax     Effort = "max"
)

// NormalizeEffort maps empty values to auto. Explicit values are returned as-is
// without validation; use ParseEffort for CLI/API input validation.
func NormalizeEffort(value Effort) Effort {
	if strings.TrimSpace(string(value)) == "" {
		return EffortAuto
	}
	return Effort(strings.TrimSpace(string(value)))
}

// ParseEffort validates a user-supplied effort string and normalizes empty to auto.
func ParseEffort(value string) (Effort, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return EffortAuto, nil
	}
	effort := Effort(trimmed)
	if !ValidEffort(effort) {
		return "", fmt.Errorf("unsupported effort %q; want auto|none|minimal|low|medium|high|xhigh|max", value)
	}
	return effort, nil
}

// ValidEffort reports whether value is one of the public effort levels.
func ValidEffort(value Effort) bool {
	switch NormalizeEffort(value) {
	case EffortAuto, EffortNone, EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return true
	default:
		return false
	}
}
