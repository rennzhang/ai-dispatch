package codex

import (
	"fmt"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/contract"
)

const desktopDispatchPrefix = "[dispatch-id:"

func uniqueDesktopPrompt(prompt string) (string, error) {
	id, err := newDesktopMessageID()
	if err != nil {
		return "", err
	}
	body, stamped := dispatchPromptBody(prompt)
	if !stamped {
		body = prompt
	}
	return fmt.Sprintf("%s\n\n%s%s]\n\n%s", contract.DispatchPromptMarker, desktopDispatchPrefix, id, body), nil
}

func desktopPromptID(prompt string) string {
	body, stamped := dispatchPromptBody(prompt)
	if !stamped || !strings.HasPrefix(body, desktopDispatchPrefix) {
		return ""
	}
	line, _, _ := strings.Cut(body, "\n")
	if !strings.HasSuffix(line, "]") {
		return ""
	}
	id := strings.TrimSuffix(strings.TrimPrefix(line, desktopDispatchPrefix), "]")
	if len(id) != 36 {
		return ""
	}
	return id
}

func desktopPromptBody(prompt string) string {
	body, stamped := dispatchPromptBody(prompt)
	if !stamped {
		return strings.TrimSpace(prompt)
	}
	if desktopPromptID(prompt) != "" {
		_, body, _ = strings.Cut(body, "\n")
	}
	return strings.TrimSpace(body)
}
