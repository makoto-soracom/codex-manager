package sessions

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// LastConversationSnippets contains the last renderable user/assistant snippets
// and the latest cumulative token usage from a session file without retaining
// the full parsed conversation.
type LastConversationSnippets struct {
	Meta                   *SessionMeta
	LastUser               string
	LastAssistant          string
	TokenUsage             TokenUsageDisplay
	HasUser                bool
	AssistantAfterLastUser bool

	lastUserIndex      int
	lastAssistantIndex int
}

// ExtractLastConversationSnippets scans a session JSONL file and keeps only the
// last renderable user and assistant messages needed by list views.
func ExtractLastConversationSnippets(path string) (LastConversationSnippets, error) {
	file, err := os.Open(path)
	if err != nil {
		return LastConversationSnippets{}, err
	}
	defer file.Close()

	session := &Session{
		Path:               path,
		subagentRequests:   map[string]string{},
		subagentNicknames:  map[string]string{},
		spawnRequestByCall: map[string]string{},
	}
	acc := snippetAccumulator{
		result: LastConversationSnippets{
			lastUserIndex:      -1,
			lastAssistantIndex: -1,
		},
	}

	reader := bufio.NewReader(file)
	lineNum := 0
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineNum++
			lineText := strings.TrimRight(string(line), "\r\n")
			if item := parseSnippetLine(lineText, lineNum, session); item != nil {
				acc.Add(*item)
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return LastConversationSnippets{}, err
		}
	}

	acc.Flush()
	acc.result.Meta = session.Meta
	acc.result.AssistantAfterLastUser = acc.result.lastAssistantIndex > acc.result.lastUserIndex
	return acc.result, nil
}

type snippetAccumulator struct {
	current *RenderItem
	index   int
	result  LastConversationSnippets
}

func (a *snippetAccumulator) Add(item RenderItem) {
	if item.Subtype == "token_count" && item.TokenUsage != nil && item.TokenUsage.HasUsage() {
		a.result.TokenUsage = *item.TokenUsage
	}

	if a.current == nil {
		a.current = &item
		return
	}

	if a.current.Type == item.Type && a.current.Subtype == item.Subtype && a.current.Role == item.Role && shouldMergeConsecutive(*a.current, item) {
		if isUserMessage(item) {
			if IsAutoContextUserMessage(a.current.Content) || IsAutoContextUserMessage(item.Content) {
				a.Flush()
				a.current = &item
				return
			}
			a.current = &item
			return
		}
		if item.EndLine > a.current.EndLine {
			a.current.EndLine = item.EndLine
		}
		if strings.TrimSpace(item.Content) != "" {
			if strings.TrimSpace(a.current.Content) != "" {
				a.current.Content = a.current.Content + "\n\n" + item.Content
			} else {
				a.current.Content = item.Content
			}
		}
		return
	}

	a.Flush()
	a.current = &item
}

func (a *snippetAccumulator) Flush() {
	if a.current == nil {
		return
	}
	item := *a.current
	switch item.Role {
	case "user":
		if !IsAutoContextUserMessage(item.Content) {
			a.result.HasUser = true
			a.result.LastUser = item.Content
			a.result.lastUserIndex = a.index
		}
	case "assistant":
		a.result.LastAssistant = item.Content
		a.result.lastAssistantIndex = a.index
	}
	a.index++
	a.current = nil
}

type snippetResponseItemPayload struct {
	Type    string            `json:"type"`
	Role    string            `json:"role"`
	Content []responseContent `json:"content"`
}

func parseSnippetLine(lineText string, lineNum int, session *Session) *RenderItem {
	var env envelope
	if err := json.Unmarshal([]byte(lineText), &env); err != nil {
		return nil
	}

	switch env.Type {
	case "session_meta":
		var meta SessionMeta
		if err := json.Unmarshal(env.Payload, &meta); err == nil {
			applyMeta(session, meta)
		}
		return nil
	case "response_item":
		return parseSnippetResponseItem(env, lineNum, session)
	case "event_msg":
		return parseSnippetEventMsg(env, lineText, lineNum, session)
	case "message":
		return parseSnippetDirectMessage(lineText, lineNum, session)
	case "reasoning":
		return parseSnippetDirectReasoning(lineText, lineNum)
	default:
		if env.Type == "" {
			if applyMetaLine(session, lineText) {
				return nil
			}
		}
		return nil
	}
}

func parseSnippetResponseItem(env envelope, lineNum int, session *Session) *RenderItem {
	var payload snippetResponseItemPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return nil
	}

	item := RenderItem{
		Line:      lineNum,
		EndLine:   lineNum,
		Timestamp: env.Timestamp,
		Type:      env.Type,
		Subtype:   payload.Type,
		Role:      "tool",
		Title:     titleForType(env.Type, payload.Type),
	}

	switch payload.Type {
	case "message":
		if payload.Role != "user" && payload.Role != "assistant" {
			return nil
		}
		item.Role = payload.Role
		item.Title = titleForRole(payload.Role)
		item.Content = extractContentText(payload.Content)
		if payload.Role == "user" {
			item.Content = trimUserRequest(item.Content)
			maybeUpdateMetaCwd(session, item.Content)
			if notification, ok := ExtractSubagentNotification(item.Content); ok {
				item.Role = "subagent"
				item.Title = "Subagent"
				item.Content = notification.StatusText
			}
			if isToolWarningUserMessage(item.Content) {
				item.Role = "assistant"
				item.Title = "Agent"
			}
		}
	case "reasoning":
		item.Role = "assistant"
		item.Title = "Reasoning"
		item.Content = extractReasoningSummary(env.Payload)
		if strings.TrimSpace(item.Content) == "" {
			return nil
		}
	default:
		if payload.Type == "" {
			return nil
		}
	}

	if strings.TrimSpace(item.Content) == "" && (item.Role == "user" || item.Role == "assistant") {
		item.Content = "(empty)"
	}
	item.Class = roleClass(item.Role)
	return &item
}

func parseSnippetDirectMessage(lineText string, lineNum int, session *Session) *RenderItem {
	var payload directMessagePayload
	if err := json.Unmarshal([]byte(lineText), &payload); err != nil {
		return nil
	}
	if payload.Role != "user" && payload.Role != "assistant" {
		return nil
	}
	item := RenderItem{
		Line:    lineNum,
		EndLine: lineNum,
		Type:    "response_item",
		Subtype: "message",
		Role:    payload.Role,
		Title:   titleForRole(payload.Role),
	}
	item.Content = extractContentText(payload.Content)
	if payload.Role == "user" {
		item.Content = trimUserRequest(item.Content)
		maybeUpdateMetaCwd(session, item.Content)
		if notification, ok := ExtractSubagentNotification(item.Content); ok {
			item.Role = "subagent"
			item.Title = "Subagent"
			item.Content = notification.StatusText
		}
		if isToolWarningUserMessage(item.Content) {
			item.Role = "assistant"
			item.Title = "Agent"
		}
	}
	if strings.TrimSpace(item.Content) == "" {
		item.Content = "(empty)"
	}
	item.Class = roleClass(item.Role)
	return &item
}

func parseSnippetDirectReasoning(lineText string, lineNum int) *RenderItem {
	content := extractReasoningSummary(json.RawMessage(lineText))
	if strings.TrimSpace(content) == "" {
		return nil
	}
	return &RenderItem{
		Line:    lineNum,
		EndLine: lineNum,
		Type:    "response_item",
		Subtype: "reasoning",
		Role:    "assistant",
		Title:   "Reasoning",
		Content: content,
		Class:   roleClass("assistant"),
	}
}

func parseSnippetEventMsg(env envelope, lineText string, lineNum int, session *Session) *RenderItem {
	var payload eventMsgPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return nil
	}
	switch payload.Type {
	case "token_count":
		var usage *TokenUsageDisplay
		if payload.Info != nil && hasTokenUsage(payload.Info.TotalTokenUsage) {
			displayUsage := tokenUsageForDisplay(payload.Info.TotalTokenUsage, nil)
			if hasTokenUsage(displayUsage) {
				usage = &TokenUsageDisplay{
					InputTokens:           displayUsage.InputTokens,
					CachedInputTokens:     displayUsage.CachedInputTokens,
					OutputTokens:          displayUsage.OutputTokens,
					ReasoningOutputTokens: displayUsage.ReasoningOutputTokens,
					TotalTokens:           displayUsage.TotalTokens,
					RateLimit:             formatRateLimit(payload.RateLimits),
				}
			}
		}
		return &RenderItem{
			Line:      lineNum,
			EndLine:   lineNum,
			Timestamp: env.Timestamp,
			Type:      env.Type,
			Subtype:   payload.Type,
			Role:      "system",
			Title:     titleForType(env.Type, payload.Type),
			Content:   payload.Type,
			Class:     roleClass("system"),
			TokenUsage: usage,
		}
	case "task_complete":
		return &RenderItem{
			Line:      lineNum,
			EndLine:   lineNum,
			Timestamp: env.Timestamp,
			Type:      env.Type,
			Subtype:   payload.Type,
			Role:      "system",
			Title:     titleForType(env.Type, payload.Type),
			Content:   payload.Type,
			Class:     roleClass("system"),
		}
	default:
		return nil
	}
}
