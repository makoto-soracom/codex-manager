package sessions

import "strings"

const (
	turnAbortedOpenTag  = "<turn_aborted>"
	turnAbortedCloseTag = "</turn_aborted>"
)

// ExtractTurnAbortedMessage returns the message inside a <turn_aborted> block.
// It only matches when the content is a single turn_aborted block.
func ExtractTurnAbortedMessage(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", false
	}
	if !strings.HasPrefix(trimmed, turnAbortedOpenTag) {
		return "", false
	}
	closeIdx := strings.Index(trimmed, turnAbortedCloseTag)
	if closeIdx == -1 {
		return "", false
	}
	rest := strings.TrimSpace(trimmed[closeIdx+len(turnAbortedCloseTag):])
	if rest != "" {
		return "", false
	}
	body := strings.TrimSpace(trimmed[len(turnAbortedOpenTag):closeIdx])
	return body, true
}
