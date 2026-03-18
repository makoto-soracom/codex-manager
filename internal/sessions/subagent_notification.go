package sessions

import (
	"encoding/json"
	"sort"
	"strings"
)

const (
	subagentNotificationOpenTag  = "<subagent_notification>"
	subagentNotificationCloseTag = "</subagent_notification>"
)

// SubagentNotification represents a parsed subagent notification block.
type SubagentNotification struct {
	AgentID    string
	StatusType string
	StatusText string
}

// ExtractSubagentNotification returns the parsed notification when the content
// is a single <subagent_notification> block.
func ExtractSubagentNotification(content string) (SubagentNotification, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || !strings.HasPrefix(trimmed, subagentNotificationOpenTag) {
		return SubagentNotification{}, false
	}

	closeIdx := strings.Index(trimmed, subagentNotificationCloseTag)
	if closeIdx == -1 {
		return SubagentNotification{}, false
	}
	if rest := strings.TrimSpace(trimmed[closeIdx+len(subagentNotificationCloseTag):]); rest != "" {
		return SubagentNotification{}, false
	}

	body := strings.TrimSpace(trimmed[len(subagentNotificationOpenTag):closeIdx])
	if body == "" {
		return SubagentNotification{}, false
	}

	var payload struct {
		AgentID string                     `json:"agent_id"`
		Status  map[string]json.RawMessage `json:"status"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return SubagentNotification{}, false
	}
	if strings.TrimSpace(payload.AgentID) == "" {
		return SubagentNotification{}, false
	}

	statusType, statusText := extractSubagentStatus(payload.Status)
	return SubagentNotification{
		AgentID:    strings.TrimSpace(payload.AgentID),
		StatusType: statusType,
		StatusText: statusText,
	}, true
}

func extractSubagentStatus(status map[string]json.RawMessage) (string, string) {
	if len(status) == 0 {
		return "", ""
	}

	preferred := []string{"completed", "failed", "running", "started", "queued"}
	for _, key := range preferred {
		raw, ok := status[key]
		if !ok {
			continue
		}
		return key, decodeSubagentStatus(raw)
	}

	keys := make([]string, 0, len(status))
	for key := range status {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	key := keys[0]
	return key, decodeSubagentStatus(status[key])
}

func decodeSubagentStatus(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}

	var payload struct {
		Text    string `json:"text"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil {
		switch {
		case strings.TrimSpace(payload.Text) != "":
			return strings.TrimSpace(payload.Text)
		case strings.TrimSpace(payload.Message) != "":
			return strings.TrimSpace(payload.Message)
		}
	}

	return strings.TrimSpace(prettyJSON(string(raw)))
}

func extractSpawnAgentRequest(arguments string) string {
	if strings.TrimSpace(arguments) == "" {
		return ""
	}

	var payload struct {
		Message string `json:"message"`
		Items   []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return ""
	}
	if strings.TrimSpace(payload.Message) != "" {
		return strings.TrimSpace(payload.Message)
	}

	parts := make([]string, 0, len(payload.Items))
	for _, item := range payload.Items {
		switch {
		case strings.TrimSpace(item.Text) != "":
			parts = append(parts, strings.TrimSpace(item.Text))
		case item.Type == "skill" && strings.TrimSpace(item.Name) != "":
			parts = append(parts, strings.TrimSpace(item.Name))
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func extractSpawnedAgent(output string) (agentID, nickname string, ok bool) {
	if strings.TrimSpace(output) == "" {
		return "", "", false
	}

	var payload struct {
		AgentID  string `json:"agent_id"`
		Nickname string `json:"nickname"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return "", "", false
	}
	if strings.TrimSpace(payload.AgentID) == "" {
		return "", "", false
	}
	return strings.TrimSpace(payload.AgentID), strings.TrimSpace(payload.Nickname), true
}

func renderSubagentSpawnOutput(output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return "(empty)"
	}
	return "```json\n" + prettyJSON(trimmed) + "\n```"
}
