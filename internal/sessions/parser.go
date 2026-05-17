package sessions

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"strconv"
	"strings"
)

// Session represents a parsed conversation file.
type Session struct {
	Path                    string
	Meta                    *SessionMeta
	Items                   []RenderItem
	subagentRequests        map[string]string
	subagentNicknames       map[string]string
	spawnRequestByCall      map[string]string
	previousTotalTokenUsage *tokenUsage
	taskStartTokenUsage     *tokenUsage
}

// SessionMeta holds metadata from session_meta entries.
type SessionMeta struct {
	ID               string             `json:"id"`
	ForkedFromID     string             `json:"forked_from_id,omitempty"`
	Timestamp        string             `json:"timestamp"`
	Cwd              string             `json:"cwd"`
	Git              *sessionMetaGit    `json:"git,omitempty"`
	Originator       string             `json:"originator"`
	CliVersion       string             `json:"cli_version"`
	Instructions     string             `json:"instructions"`
	AgentNickname    string             `json:"agent_nickname,omitempty"`
	AgentRole        string             `json:"agent_role,omitempty"`
	Source           *sessionMetaSource `json:"source,omitempty"`
	BaseInstructions *instructionText   `json:"base_instructions,omitempty"`
}

type sessionMetaSource struct {
	Subagent *sessionMetaSubagentSource `json:"subagent,omitempty"`
}

type sessionMetaSubagentSource struct {
	ThreadSpawn *sessionMetaThreadSpawn `json:"thread_spawn,omitempty"`
}

type sessionMetaThreadSpawn struct {
	ParentThreadID string `json:"parent_thread_id,omitempty"`
	Depth          int    `json:"depth,omitempty"`
	AgentNickname  string `json:"agent_nickname,omitempty"`
	AgentRole      string `json:"agent_role,omitempty"`
}

type sessionMetaGit struct {
	CommitHash    string `json:"commit_hash,omitempty"`
	Branch        string `json:"branch,omitempty"`
	RepositoryURL string `json:"repository_url,omitempty"`
}

func (s *sessionMetaSource) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] == '"' {
		*s = sessionMetaSource{}
		return nil
	}
	type alias sessionMetaSource
	var decoded alias
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return err
	}
	*s = sessionMetaSource(decoded)
	return nil
}

// RenderItem is a display-ready entry for the HTML view.
type RenderItem struct {
	Line               int
	EndLine            int
	Timestamp          string
	Type               string
	Subtype            string
	CallID             string
	ToolName           string
	ToolStatus         string
	ToolInput          string
	Role               string
	Title              string
	Content            string
	Raw                string
	Class              string
	SubagentID         string
	SubagentNickname   string
	SubagentStatusType string
	SubagentRequest    string
	TokenUsage         *TokenUsageDisplay
}

type envelope struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type responseContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseItemPayload struct {
	Type      string            `json:"type"`
	Role      string            `json:"role"`
	Phase     string            `json:"phase"`
	Content   []responseContent `json:"content"`
	Name      string            `json:"name"`
	Arguments string            `json:"arguments"`
	CallID    string            `json:"call_id"`
	Output    string            `json:"output"`
	Status    string            `json:"status"`
	Input     string            `json:"input"`
	Action    json.RawMessage   `json:"action"`
}

type eventMsgPayload struct {
	Type       string          `json:"type"`
	Message    string          `json:"message"`
	Info       *tokenCountInfo `json:"info"`
	RateLimits *rateLimitInfo  `json:"rate_limits"`
	DurationMS int64           `json:"duration_ms"`
}

type tokenCountInfo struct {
	TotalTokenUsage  tokenUsage `json:"total_token_usage"`
	LastTokenUsage   tokenUsage `json:"last_token_usage"`
	ModelContextSize int        `json:"model_context_window"`
}

type tokenUsage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
	TotalTokens           int `json:"total_tokens"`
}

// TokenUsageDisplay is the display-ready token usage for a token_count event.
type TokenUsageDisplay struct {
	InputTokens           int
	CachedInputTokens     int
	OutputTokens          int
	ReasoningOutputTokens int
	TotalTokens           int
	RateLimit             string
}

func (usage TokenUsageDisplay) Add(other TokenUsageDisplay) TokenUsageDisplay {
	usage.InputTokens += other.InputTokens
	usage.CachedInputTokens += other.CachedInputTokens
	usage.OutputTokens += other.OutputTokens
	usage.ReasoningOutputTokens += other.ReasoningOutputTokens
	usage.TotalTokens += other.TotalTokens
	if usage.RateLimit == "" {
		usage.RateLimit = other.RateLimit
	}
	return usage
}

func (usage TokenUsageDisplay) Format(includeRateLimit bool) string {
	parts := []string{
		"input=" + formatTokenCount(usage.InputTokens),
		"cached=" + formatTokenCount(usage.CachedInputTokens),
		"output=" + formatTokenCount(usage.OutputTokens) + " (reasoning=" + formatTokenCount(usage.ReasoningOutputTokens) + ")",
		"total=" + formatTokenCount(usage.TotalTokens),
	}
	if includeRateLimit && strings.TrimSpace(usage.RateLimit) != "" {
		parts = append(parts, "rate limit="+strings.TrimSpace(usage.RateLimit))
	}
	return strings.Join(parts, ", ")
}

func (usage TokenUsageDisplay) FormatTotal() string {
	return "total=" + formatTokenCount(usage.TotalTokens)
}

func (usage TokenUsageDisplay) HasUsage() bool {
	return usage.InputTokens > 0 ||
		usage.CachedInputTokens > 0 ||
		usage.OutputTokens > 0 ||
		usage.ReasoningOutputTokens > 0 ||
		usage.TotalTokens > 0
}

type rateLimitInfo struct {
	LimitID              string `json:"limit_id"`
	LimitName            string `json:"limit_name"`
	PlanType             string `json:"plan_type"`
	RateLimitReachedType string `json:"rate_limit_reached_type"`
}

type instructionText struct {
	Text string `json:"text"`
}

type directMessagePayload struct {
	Type    string            `json:"type"`
	Role    string            `json:"role"`
	Content []responseContent `json:"content"`
}

type metaLinePayload struct {
	ID               string             `json:"id"`
	ForkedFromID     string             `json:"forked_from_id,omitempty"`
	Timestamp        string             `json:"timestamp"`
	Cwd              string             `json:"cwd"`
	Git              *sessionMetaGit    `json:"git,omitempty"`
	Originator       string             `json:"originator"`
	CliVersion       string             `json:"cli_version"`
	AgentNickname    string             `json:"agent_nickname,omitempty"`
	AgentRole        string             `json:"agent_role,omitempty"`
	Source           *sessionMetaSource `json:"source,omitempty"`
	Instructions     *string            `json:"instructions"`
	BaseInstructions *instructionText   `json:"base_instructions"`
}

func (m *SessionMeta) ParentThreadID() string {
	if m == nil {
		return ""
	}
	if m.Source != nil && m.Source.Subagent != nil && m.Source.Subagent.ThreadSpawn != nil {
		if id := strings.TrimSpace(m.Source.Subagent.ThreadSpawn.ParentThreadID); id != "" {
			return id
		}
	}
	return strings.TrimSpace(m.ForkedFromID)
}

func (m *SessionMeta) SubagentNicknameValue() string {
	if m == nil {
		return ""
	}
	if nickname := strings.TrimSpace(m.AgentNickname); nickname != "" {
		return nickname
	}
	if m.Source != nil && m.Source.Subagent != nil && m.Source.Subagent.ThreadSpawn != nil {
		return strings.TrimSpace(m.Source.Subagent.ThreadSpawn.AgentNickname)
	}
	return ""
}

func (m *SessionMeta) SubagentRoleValue() string {
	if m == nil {
		return ""
	}
	if role := strings.TrimSpace(m.AgentRole); role != "" {
		return role
	}
	if m.Source != nil && m.Source.Subagent != nil && m.Source.Subagent.ThreadSpawn != nil {
		return strings.TrimSpace(m.Source.Subagent.ThreadSpawn.AgentRole)
	}
	return ""
}

func (m *SessionMeta) IsSubagentThread() bool {
	if m == nil {
		return false
	}
	return m.ParentThreadID() != "" || m.SubagentNicknameValue() != "" || m.SubagentRoleValue() != ""
}

func (m *SessionMeta) GitBranch() string {
	if m == nil || m.Git == nil {
		return ""
	}
	return strings.TrimSpace(m.Git.Branch)
}

func (m *SessionMeta) GitRepositoryURL() string {
	if m == nil || m.Git == nil {
		return ""
	}
	return strings.TrimSpace(m.Git.RepositoryURL)
}

// ParseSession reads a jsonl file and returns a parsed Session.
func ParseSession(path string) (*Session, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	session := &Session{
		Path:               path,
		subagentRequests:   map[string]string{},
		subagentNicknames:  map[string]string{},
		spawnRequestByCall: map[string]string{},
	}
	reader := bufio.NewReader(file)
	lineNum := 0

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineNum++
			lineText := strings.TrimRight(string(line), "\r\n")
			item := parseLine(lineText, lineNum, session)
			if item != nil {
				session.Items = append(session.Items, *item)
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	session.Items = mergeConsecutive(session.Items)

	return session, nil
}

func parseLine(lineText string, lineNum int, session *Session) *RenderItem {
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
		return parseResponseItem(env, lineText, lineNum, session)
	case "event_msg":
		return parseEventMsg(env, lineText, lineNum, session)
	case "message":
		return parseDirectMessage(lineText, lineNum, session)
	case "reasoning":
		return parseDirectReasoning(lineText, lineNum)
	default:
		if env.Type == "" {
			if applyMetaLine(session, lineText) {
				return nil
			}
		}
		return nil
	}
}

func parseResponseItem(env envelope, lineText string, lineNum int, session *Session) *RenderItem {
	var payload responseItemPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return nil
	}

	item := RenderItem{
		Line:      lineNum,
		EndLine:   lineNum,
		Timestamp: env.Timestamp,
		Type:      env.Type,
		Subtype:   payload.Type,
		CallID:    payload.CallID,
		Role:      payload.Role,
		Title:     titleForType(env.Type, payload.Type),
		Raw:       lineText,
	}

	switch payload.Type {
	case "function_call":
		if payload.Name == "spawn_agent" && session != nil && payload.CallID != "" {
			if request := extractSpawnAgentRequest(payload.Arguments); request != "" {
				session.spawnRequestByCall[payload.CallID] = request
			}
		}
		item.ToolName = payload.Name
		item.ToolInput = payload.Arguments
		item.Role = "tool"
		item.Class = roleClass("tool")
		item.Content = renderFunctionCallContent(payload.Name, payload.CallID, payload.Arguments)
	case "function_call_output":
		if session != nil && payload.CallID != "" {
			if agentID, nickname, ok := extractSpawnedAgent(payload.Output); ok {
				if request := session.spawnRequestByCall[payload.CallID]; request != "" {
					session.subagentRequests[agentID] = request
				}
				if nickname != "" {
					session.subagentNicknames[agentID] = nickname
				}
				item.Role = "subagent"
				item.Title = "Subagent"
				item.Content = renderSubagentSpawnOutput(payload.Output)
				item.SubagentID = agentID
				item.SubagentNickname = nickname
				item.SubagentStatusType = "spawned"
				item.SubagentRequest = session.subagentRequests[agentID]
				item.Class = roleClass("subagent")
				return &item
			}
		}
		item.Role = "tool"
		item.Class = roleClass("tool")
		item.Content = renderFunctionCallOutputContent(payload.CallID, payload.Output)
	case "message":
		if payload.Role != "user" && payload.Role != "assistant" {
			return nil
		}
		if payload.Role == "user" {
			item.Title = "User"
		} else {
			item.Title = "Agent"
		}
		item.Content = extractContentText(payload.Content)
		if payload.Role == "user" {
			item.Content = trimUserRequest(item.Content)
			maybeUpdateMetaCwd(session, item.Content)
		}
		if payload.Role == "user" {
			if notification, ok := ExtractSubagentNotification(item.Content); ok {
				item.Role = "subagent"
				item.Title = "Subagent"
				item.Content = notification.StatusText
				item.SubagentID = notification.AgentID
				item.SubagentNickname = session.subagentNicknames[notification.AgentID]
				item.SubagentStatusType = notification.StatusType
				item.SubagentRequest = session.subagentRequests[notification.AgentID]
			}
		}
		if item.Content == "" {
			item.Content = prettyJSON(string(env.Payload))
		}
		item.Class = roleClass(item.Role)
		if payload.Role == "user" && isToolWarningUserMessage(item.Content) {
			item.Role = "assistant"
			item.Title = "Agent"
			item.Class = roleClass("assistant")
		}
	case "reasoning":
		item.Role = "assistant"
		item.Class = roleClass("assistant")
		item.Content = extractReasoningSummary(env.Payload)
		if strings.TrimSpace(item.Content) == "" {
			return nil
		}
	case "web_search_call":
		item.Role = "tool"
		item.Class = roleClass("tool")
		item.Content = renderWebSearchCallContent(env.Payload)
		if strings.TrimSpace(item.Content) == "" {
			return nil
		}
	case "custom_tool_call":
		item.ToolName = payload.Name
		item.ToolStatus = payload.Status
		item.ToolInput = payload.Input
		item.Role = "tool"
		item.Class = roleClass("tool")
		item.Content = renderCustomToolCallContent(payload.Name, payload.Status, payload.CallID, payload.Input)
	case "custom_tool_call_output":
		item.Role = "tool"
		item.Class = roleClass("tool")
		item.Content = renderCustomToolCallOutputContent(payload.CallID, payload.Output)
	case "ghost_snapshot":
		item.Role = "system"
		item.Class = roleClass("system")
		item.Content = renderGhostSnapshotContent(env.Payload)
	default:
		return nil
	}

	if strings.TrimSpace(item.Content) == "" {
		item.Content = fencedJSONBlock(string(env.Payload))
	}
	if strings.TrimSpace(item.Content) == "" {
		item.Content = "(empty)"
	}

	return &item
}

func parseDirectMessage(lineText string, lineNum int, session *Session) *RenderItem {
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
			item.SubagentID = notification.AgentID
			item.SubagentStatusType = notification.StatusType
			if session != nil {
				item.SubagentNickname = session.subagentNicknames[notification.AgentID]
				item.SubagentRequest = session.subagentRequests[notification.AgentID]
			}
		}
	}
	if item.Content == "" {
		item.Content = prettyJSON(lineText)
	}
	item.Class = roleClass(item.Role)
	if payload.Role == "user" && isToolWarningUserMessage(item.Content) {
		item.Role = "assistant"
		item.Title = "Agent"
		item.Class = roleClass("assistant")
	}
	return &item
}

func parseDirectReasoning(lineText string, lineNum int) *RenderItem {
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

func parseEventMsg(env envelope, lineText string, lineNum int, session *Session) *RenderItem {
	var payload eventMsgPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return nil
	}
	switch payload.Type {
	case "token_count":
		return parseTokenCountEvent(env, lineText, lineNum, session, payload)
	case "task_started":
		if session != nil {
			session.taskStartTokenUsage = cloneTokenUsage(session.previousTotalTokenUsage)
		}
		return nil
	case "task_complete":
		return parseTaskCompleteEvent(env, lineText, lineNum, session, payload)
	default:
		return nil
	}
}

func parseTokenCountEvent(env envelope, lineText string, lineNum int, session *Session, payload eventMsgPayload) *RenderItem {
	var previous *tokenUsage
	if session != nil && session.previousTotalTokenUsage != nil {
		usage := *session.previousTotalTokenUsage
		previous = &usage
	}
	tokenUsage, content := renderTokenCountContent(payload.Info, payload.RateLimits, previous)
	if session != nil && payload.Info != nil && hasTokenUsage(payload.Info.TotalTokenUsage) {
		usage := payload.Info.TotalTokenUsage
		session.previousTotalTokenUsage = &usage
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}

	return &RenderItem{
		Line:       lineNum,
		EndLine:    lineNum,
		Timestamp:  env.Timestamp,
		Type:       env.Type,
		Subtype:    payload.Type,
		Title:      titleForType(env.Type, payload.Type),
		Content:    content,
		Raw:        lineText,
		Role:       "system",
		Class:      roleClass("system"),
		TokenUsage: tokenUsage,
	}
}

func parseTaskCompleteEvent(env envelope, lineText string, lineNum int, session *Session, payload eventMsgPayload) *RenderItem {
	var usage *TokenUsageDisplay
	if session != nil && session.previousTotalTokenUsage != nil {
		displayUsage := tokenUsageForDisplay(*session.previousTotalTokenUsage, session.taskStartTokenUsage)
		if hasTokenUsage(displayUsage) {
			usage = &TokenUsageDisplay{
				InputTokens:           displayUsage.InputTokens,
				CachedInputTokens:     displayUsage.CachedInputTokens,
				OutputTokens:          displayUsage.OutputTokens,
				ReasoningOutputTokens: displayUsage.ReasoningOutputTokens,
				TotalTokens:           displayUsage.TotalTokens,
			}
		}
		session.taskStartTokenUsage = nil
	}
	content := formatTaskSummary(payload.DurationMS, usage)
	if strings.TrimSpace(content) == "" {
		return nil
	}
	return &RenderItem{
		Line:       lineNum,
		EndLine:    lineNum,
		Timestamp:  env.Timestamp,
		Type:       env.Type,
		Subtype:    payload.Type,
		Title:      titleForType(env.Type, payload.Type),
		Content:    content,
		Raw:        lineText,
		Role:       "system",
		Class:      strings.TrimSpace(roleClass("system") + " task-summary"),
		TokenUsage: usage,
	}
}

func renderTokenCountContent(info *tokenCountInfo, limits *rateLimitInfo, previous *tokenUsage) (*TokenUsageDisplay, string) {
	var display *TokenUsageDisplay
	if info != nil && hasTokenUsage(info.LastTokenUsage) {
		current := info.LastTokenUsage
		if previous != nil {
			current = info.TotalTokenUsage
		}
		usage := tokenUsageForDisplay(current, previous)
		if hasTokenUsage(usage) {
			display = &TokenUsageDisplay{
				InputTokens:           usage.InputTokens,
				CachedInputTokens:     usage.CachedInputTokens,
				OutputTokens:          usage.OutputTokens,
				ReasoningOutputTokens: usage.ReasoningOutputTokens,
				TotalTokens:           usage.TotalTokens,
				RateLimit:             formatRateLimit(limits),
			}
		}
	}
	if display == nil && !hasReachedRateLimit(limits) {
		return nil, ""
	}
	parts := make([]string, 0, 2)
	if display != nil {
		parts = append(parts, display.Format(false))
	}
	if value := formatRateLimit(limits); value != "" {
		parts = append(parts, "rate limit="+value)
	}
	return display, strings.Join(parts, ", ")
}

func cloneTokenUsage(usage *tokenUsage) *tokenUsage {
	if usage == nil {
		return nil
	}
	cloned := *usage
	return &cloned
}

func tokenUsageForDisplay(current tokenUsage, previous *tokenUsage) tokenUsage {
	display := tokenUsage{
		InputTokens:           nonCachedInputTokens(current),
		CachedInputTokens:     current.CachedInputTokens,
		OutputTokens:          current.OutputTokens,
		ReasoningOutputTokens: current.ReasoningOutputTokens,
		TotalTokens:           current.TotalTokens,
	}
	if previous == nil {
		return display
	}
	rawDelta := tokenUsage{
		InputTokens:           current.InputTokens - previous.InputTokens,
		CachedInputTokens:     current.CachedInputTokens - previous.CachedInputTokens,
		OutputTokens:          current.OutputTokens - previous.OutputTokens,
		ReasoningOutputTokens: current.ReasoningOutputTokens - previous.ReasoningOutputTokens,
		TotalTokens:           current.TotalTokens - previous.TotalTokens,
	}
	return tokenUsage{
		InputTokens:           nonCachedInputTokens(rawDelta),
		CachedInputTokens:     rawDelta.CachedInputTokens,
		OutputTokens:          rawDelta.OutputTokens,
		ReasoningOutputTokens: rawDelta.ReasoningOutputTokens,
		TotalTokens:           rawDelta.TotalTokens,
	}
}

func hasTokenUsage(usage tokenUsage) bool {
	return usage.InputTokens > 0 ||
		usage.CachedInputTokens > 0 ||
		usage.OutputTokens > 0 ||
		usage.ReasoningOutputTokens > 0 ||
		usage.TotalTokens > 0
}

func nonCachedInputTokens(usage tokenUsage) int {
	value := usage.InputTokens - usage.CachedInputTokens
	if value < 0 {
		return 0
	}
	return value
}

func formatRateLimit(limits *rateLimitInfo) string {
	if limits == nil {
		return ""
	}
	name := strings.TrimSpace(limits.LimitName)
	if name == "" {
		name = strings.TrimSpace(limits.LimitID)
	}
	plan := strings.TrimSpace(limits.PlanType)
	reached := strings.TrimSpace(limits.RateLimitReachedType)
	if name == "" && plan == "" && reached == "" {
		return ""
	}
	value := ""
	if name != "" {
		value = name
	}
	if plan != "" {
		value += " (" + plan + ")"
	}
	if reached == "" {
		return strings.TrimSpace(value)
	}
	if value == "" {
		return reached
	}
	return value + ", reached=" + reached
}

func hasReachedRateLimit(limits *rateLimitInfo) bool {
	if limits == nil {
		return false
	}
	return strings.TrimSpace(limits.RateLimitReachedType) != ""
}

func formatTaskSummary(durationMS int64, usage *TokenUsageDisplay) string {
	duration := formatTaskDuration(durationMS)
	switch {
	case duration != "" && usage != nil && usage.HasUsage():
		return "Worked for " + duration + " (" + usage.Format(false) + ")"
	case duration != "":
		return "Worked for " + duration
	case usage != nil && usage.HasUsage():
		return usage.Format(false)
	default:
		return ""
	}
}

func formatTaskDuration(durationMS int64) string {
	if durationMS <= 0 {
		return ""
	}
	totalSeconds := durationMS / 1000
	if totalSeconds <= 0 {
		return "0s"
	}
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	parts := make([]string, 0, 3)
	if hours > 0 {
		parts = append(parts, strconv.FormatInt(hours, 10)+"h")
	}
	if minutes > 0 {
		parts = append(parts, strconv.FormatInt(minutes, 10)+"m")
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, strconv.FormatInt(seconds, 10)+"s")
	}
	return strings.Join(parts, " ")
}

func formatTokenCount(value int) string {
	if value < 0 {
		return "-" + formatTokenCount(-value)
	}
	text := strconv.Itoa(value)
	if len(text) <= 3 {
		return text
	}
	first := len(text) % 3
	if first == 0 {
		first = 3
	}
	var builder strings.Builder
	builder.Grow(len(text) + (len(text)-1)/3)
	builder.WriteString(text[:first])
	for index := first; index < len(text); index += 3 {
		builder.WriteByte(',')
		builder.WriteString(text[index : index+3])
	}
	return builder.String()
}

func extractContentText(contents []responseContent) string {
	if len(contents) == 0 {
		return ""
	}
	parts := make([]string, 0, len(contents))
	for _, item := range contents {
		if item.Text == "" {
			continue
		}
		parts = append(parts, item.Text)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func extractReasoningSummary(raw json.RawMessage) string {
	var payload struct {
		Summary []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if len(payload.Summary) == 0 {
		return ""
	}
	parts := make([]string, 0, len(payload.Summary))
	for _, item := range payload.Summary {
		if item.Text == "" {
			continue
		}
		parts = append(parts, item.Text)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func renderFunctionCallContent(name, callID, arguments string) string {
	if strings.TrimSpace(name) == "exec_command" {
		if content := renderExecCommandCallContent(callID, arguments); content != "" {
			return content
		}
	}
	if strings.TrimSpace(name) == "update_plan" {
		if content := renderUpdatePlanCallContent(callID, arguments); content != "" {
			return content
		}
	}
	sections := make([]string, 0, 3)
	if value := strings.TrimSpace(name); value != "" {
		sections = append(sections, "**Tool:** "+value)
	}
	if value := strings.TrimSpace(callID); value != "" {
		sections = append(sections, "**Call ID:** "+value)
	}
	if block := renderLabeledCodeBlock("Arguments", arguments); block != "" {
		sections = append(sections, block)
	}
	return joinMarkdownSections(sections...)
}

type updatePlanArgs struct {
	Explanation string           `json:"explanation"`
	Plan        []updatePlanStep `json:"plan"`
}

type updatePlanStep struct {
	Status string `json:"status"`
	Step   string `json:"step"`
}

func renderUpdatePlanCallContent(callID, arguments string) string {
	var payload updatePlanArgs
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return ""
	}

	sections := make([]string, 0, 4)
	sections = append(sections, "**Tool:** update_plan")
	if value := strings.TrimSpace(callID); value != "" {
		sections = append(sections, "**Call ID:** "+value)
	}
	if value := strings.TrimSpace(payload.Explanation); value != "" {
		sections = append(sections, renderLabeledPlainText("Explanation", value))
	}
	if plan := renderUpdatePlanMarkdown(payload.Plan); plan != "" {
		sections = append(sections, plan)
	}
	return joinMarkdownSections(sections...)
}

func renderUpdatePlanMarkdown(steps []updatePlanStep) string {
	items := make([]string, 0, len(steps))
	for _, step := range steps {
		text := strings.TrimSpace(step.Step)
		if text == "" {
			continue
		}
		items = append(items, fmt.Sprintf("- %s %s", updatePlanStatusMarker(step.Status), text))
	}
	if len(items) == 0 {
		return ""
	}
	return fmt.Sprintf("**Plan**\n%s", strings.Join(items, "\n"))
}

func updatePlanStatusMarker(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		return "✅"
	case "in_progress":
		return "□"
	case "pending":
		return "□"
	default:
		value := strings.TrimSpace(status)
		if value == "" {
			return "□"
		}
		return "[" + value + "]"
	}
}

func renderExecCommandCallContent(callID, arguments string) string {
	type execCommandArgs struct {
		Cmd                string `json:"cmd"`
		Workdir            string `json:"workdir"`
		Justification      string `json:"justification"`
		SandboxPermissions string `json:"sandbox_permissions"`
	}

	var payload execCommandArgs
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return ""
	}

	sections := make([]string, 0, 5)
	sections = append(sections, "**Tool:** exec_command")
	if value := strings.TrimSpace(callID); value != "" {
		sections = append(sections, "**Call ID:** "+value)
	}
	if block := renderLabeledShellBlock("Command", payload.Cmd); block != "" {
		sections = append(sections, block)
	}
	if value := strings.TrimSpace(payload.Workdir); value != "" {
		sections = append(sections, "**Workdir:** `"+value+"`")
	}
	if value := strings.TrimSpace(payload.Justification); value != "" {
		sections = append(sections, "**Justification:** "+value)
	}
	if value := strings.TrimSpace(payload.SandboxPermissions); value != "" && value != "use_default" {
		sections = append(sections, "**Sandbox:** "+value)
	}
	return joinMarkdownSections(sections...)
}

func renderFunctionCallOutputContent(callID, output string) string {
	sections := make([]string, 0, 2)
	if value := strings.TrimSpace(callID); value != "" {
		sections = append(sections, "**Call ID:** "+value)
	}
	if text, ok := extractFunctionCallOutputText(output); ok {
		sections = append(sections, renderLabeledPlainText("Output", text))
	} else if block := renderLabeledCodeBlock("Output", output); block != "" {
		sections = append(sections, block)
	}
	return joinMarkdownSections(sections...)
}

func renderCustomToolCallContent(name, status, callID, input string) string {
	sections := make([]string, 0, 4)
	if value := strings.TrimSpace(name); value != "" {
		sections = append(sections, "**Custom tool:** "+value)
	}
	if value := strings.TrimSpace(status); value != "" {
		sections = append(sections, "**Status:** "+value)
	}
	if value := strings.TrimSpace(callID); value != "" {
		sections = append(sections, "**Call ID:** "+value)
	}
	if block := renderCustomToolCallInput(name, input); block != "" {
		sections = append(sections, block)
	}
	return joinMarkdownSections(sections...)
}

func renderCustomToolCallInput(name, input string) string {
	if strings.TrimSpace(name) == "apply_patch" {
		return renderLabeledDiffBlock("Patch", input)
	}
	return renderLabeledCodeBlock("Input", input)
}

func renderCustomToolCallOutputContent(callID, output string) string {
	sections := make([]string, 0, 2)
	if value := strings.TrimSpace(callID); value != "" {
		sections = append(sections, "**Call ID:** "+value)
	}
	if text, ok := extractCustomToolOutputText(output); ok {
		sections = append(sections, renderLabeledPlainText("Output", text))
	} else if block := renderLabeledCodeBlock("Output", output); block != "" {
		sections = append(sections, block)
	}
	return joinMarkdownSections(sections...)
}

func renderWebSearchCallContent(raw json.RawMessage) string {
	var payload struct {
		Status string `json:"status"`
		Action struct {
			Type    string   `json:"type"`
			Query   string   `json:"query"`
			Queries []string `json:"queries"`
		} `json:"action"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if strings.TrimSpace(payload.Action.Query) == "" && countNonEmptyStrings(payload.Action.Queries) == 0 {
		return ""
	}

	sections := make([]string, 0, 4)
	if value := strings.TrimSpace(payload.Status); value != "" {
		sections = append(sections, "**Status:** "+value)
	}
	if value := strings.TrimSpace(payload.Action.Type); value != "" {
		sections = append(sections, "**Action:** "+value)
	}
	if block := renderLabeledCodeBlock("Query", payload.Action.Query); block != "" {
		sections = append(sections, block)
	}
	if list := renderMarkdownList("Expanded queries", payload.Action.Queries); list != "" {
		sections = append(sections, list)
	}
	return joinMarkdownSections(sections...)
}

func countNonEmptyStrings(values []string) int {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}

func renderGhostSnapshotContent(raw json.RawMessage) string {
	var payload struct {
		GhostCommit struct {
			ID                        string   `json:"id"`
			Parent                    string   `json:"parent"`
			PreexistingUntrackedDirs  []string `json:"preexisting_untracked_dirs"`
			PreexistingUntrackedFiles []string `json:"preexisting_untracked_files"`
		} `json:"ghost_commit"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}

	sections := make([]string, 0, 4)
	if value := strings.TrimSpace(payload.GhostCommit.ID); value != "" {
		sections = append(sections, "**Commit:** "+value)
	}
	if value := strings.TrimSpace(payload.GhostCommit.Parent); value != "" {
		sections = append(sections, "**Parent:** "+value)
	}
	if list := renderMarkdownList("Preexisting untracked files", payload.GhostCommit.PreexistingUntrackedFiles); list != "" {
		sections = append(sections, list)
	} else {
		sections = append(sections, "**Preexisting untracked files:** 0")
	}
	if list := renderMarkdownList("Preexisting untracked directories", payload.GhostCommit.PreexistingUntrackedDirs); list != "" {
		sections = append(sections, list)
	} else {
		sections = append(sections, "**Preexisting untracked directories:** 0")
	}
	return joinMarkdownSections(sections...)
}

func renderLabeledCodeBlock(label, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if pretty, ok := prettyJSONString(trimmed); ok {
		return fmt.Sprintf("**%s**\n%s", label, fencedCodeBlock("json", pretty))
	}
	return fmt.Sprintf("**%s**\n%s", label, fencedCodeBlock("", trimmed))
}

func renderLabeledShellBlock(label, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("**%s**\n%s", label, fencedCodeBlock("sh", trimmed))
}

func renderLabeledDiffBlock(label, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("**%s**\n%s", label, fencedCodeBlock("diff", trimmed))
}

func renderLabeledPlainText(label, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("**%s**\n%s", label, preserveMarkdownLineBreaks(html.EscapeString(trimmed)))
}

func renderMarkdownList(label string, values []string) string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		items = append(items, "- "+value)
	}
	if len(items) == 0 {
		return ""
	}
	return fmt.Sprintf("**%s**\n%s", label, strings.Join(items, "\n"))
}

func joinMarkdownSections(sections ...string) string {
	filtered := make([]string, 0, len(sections))
	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}
		filtered = append(filtered, section)
	}
	return strings.Join(filtered, "\n\n")
}

func preserveMarkdownLineBreaks(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, "\r")
	}
	return strings.Join(lines, "  \n")
}

func prettyJSONString(raw string) (string, bool) {
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return "", false
	}
	formatted, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		return "", false
	}
	return string(formatted), true
}

func fencedJSONBlock(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	return fencedCodeBlock("json", prettyJSON(trimmed))
}

func fencedCodeBlock(language, value string) string {
	fence := markdownFence(value)
	if language != "" {
		return fence + language + "\n" + value + "\n" + fence
	}
	return fence + "\n" + value + "\n" + fence
}

func markdownFence(value string) string {
	maxRun := 0
	currentRun := 0
	for _, r := range value {
		if r == '`' {
			currentRun++
			if currentRun > maxRun {
				maxRun = currentRun
			}
			continue
		}
		currentRun = 0
	}
	if maxRun < 3 {
		maxRun = 3
	} else {
		maxRun++
	}
	return strings.Repeat("`", maxRun)
}

func extractCustomToolOutputText(output string) (string, bool) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return "", false
	}
	var payload struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		sanitized := strings.NewReplacer("\r\n", "\\n", "\n", "\\n", "\r", "\\r").Replace(trimmed)
		if err := json.Unmarshal([]byte(sanitized), &payload); err != nil {
			return "", false
		}
	}
	if strings.TrimSpace(payload.Output) == "" {
		return "", false
	}
	return payload.Output, true
}

func extractFunctionCallOutputText(output string) (string, bool) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return "", false
	}

	if texts, ok := extractTextPayloads(trimmed); ok {
		return strings.Join(texts, "\n\n"), true
	}
	return "", false
}

type toolOutputTextPayload struct {
	Type    string                  `json:"type"`
	Text    string                  `json:"text"`
	Content []toolOutputTextPayload `json:"content"`
}

func extractTextPayloads(raw string) ([]string, bool) {
	var list []toolOutputTextPayload
	if err := json.Unmarshal([]byte(raw), &list); err == nil {
		if texts := collectTextPayloads(list); len(texts) > 0 {
			return texts, true
		}
	}

	var single toolOutputTextPayload
	if err := json.Unmarshal([]byte(raw), &single); err == nil {
		if texts := collectTextPayloads(single.Content); len(texts) > 0 {
			return texts, true
		}
		if strings.TrimSpace(single.Text) != "" && isTextPayloadType(single.Type) {
			return []string{single.Text}, true
		}
	}

	return nil, false
}

func collectTextPayloads(items []toolOutputTextPayload) []string {
	texts := make([]string, 0, len(items))
	for _, item := range items {
		if nested := collectTextPayloads(item.Content); len(nested) > 0 {
			texts = append(texts, nested...)
		}
		if strings.TrimSpace(item.Text) == "" {
			continue
		}
		if !isTextPayloadType(item.Type) {
			continue
		}
		texts = append(texts, item.Text)
	}
	return texts
}

func isTextPayloadType(payloadType string) bool {
	switch strings.TrimSpace(payloadType) {
	case "", "text", "input_text", "output_text", "summary_text":
		return true
	default:
		return false
	}
}

func applyMeta(session *Session, meta SessionMeta) {
	if session == nil {
		return
	}
	if session.Meta == nil {
		session.Meta = &SessionMeta{}
	}
	mergeMeta(session.Meta, meta)
}

func mergeMeta(target *SessionMeta, meta SessionMeta) {
	if target == nil {
		return
	}
	if target.ID == "" {
		target.ID = meta.ID
	}
	if target.ForkedFromID == "" {
		target.ForkedFromID = meta.ForkedFromID
	}
	if target.Timestamp == "" {
		target.Timestamp = meta.Timestamp
	}
	if target.Cwd == "" {
		target.Cwd = meta.Cwd
	}
	if target.Git == nil {
		target.Git = meta.Git
	} else {
		mergeSessionMetaGit(target.Git, meta.Git)
	}
	if target.Originator == "" {
		target.Originator = meta.Originator
	}
	if target.CliVersion == "" {
		target.CliVersion = meta.CliVersion
	}
	if target.AgentNickname == "" {
		target.AgentNickname = meta.AgentNickname
	}
	if target.AgentRole == "" {
		target.AgentRole = meta.AgentRole
	}
	if target.Source == nil {
		target.Source = meta.Source
	} else {
		mergeSessionMetaSource(target.Source, meta.Source)
	}
	if target.Instructions == "" {
		switch {
		case meta.Instructions != "":
			target.Instructions = meta.Instructions
		case meta.BaseInstructions != nil:
			target.Instructions = meta.BaseInstructions.Text
		}
	}
}

func mergeSessionMetaSource(target, src *sessionMetaSource) {
	if target == nil || src == nil {
		return
	}
	if target.Subagent == nil {
		target.Subagent = src.Subagent
		return
	}
	if src.Subagent == nil {
		return
	}
	if target.Subagent.ThreadSpawn == nil {
		target.Subagent.ThreadSpawn = src.Subagent.ThreadSpawn
		return
	}
	if src.Subagent.ThreadSpawn == nil {
		return
	}
	if target.Subagent.ThreadSpawn.ParentThreadID == "" {
		target.Subagent.ThreadSpawn.ParentThreadID = src.Subagent.ThreadSpawn.ParentThreadID
	}
	if target.Subagent.ThreadSpawn.Depth == 0 {
		target.Subagent.ThreadSpawn.Depth = src.Subagent.ThreadSpawn.Depth
	}
	if target.Subagent.ThreadSpawn.AgentNickname == "" {
		target.Subagent.ThreadSpawn.AgentNickname = src.Subagent.ThreadSpawn.AgentNickname
	}
	if target.Subagent.ThreadSpawn.AgentRole == "" {
		target.Subagent.ThreadSpawn.AgentRole = src.Subagent.ThreadSpawn.AgentRole
	}
}

func mergeSessionMetaGit(target, src *sessionMetaGit) {
	if target == nil || src == nil {
		return
	}
	if target.CommitHash == "" {
		target.CommitHash = src.CommitHash
	}
	if target.Branch == "" {
		target.Branch = src.Branch
	}
	if target.RepositoryURL == "" {
		target.RepositoryURL = src.RepositoryURL
	}
}

func applyMetaLine(session *Session, lineText string) bool {
	var meta metaLinePayload
	if err := json.Unmarshal([]byte(lineText), &meta); err != nil {
		return false
	}
	if meta.ID == "" && meta.ForkedFromID == "" && meta.Timestamp == "" && meta.Cwd == "" && meta.Git == nil && meta.Originator == "" && meta.CliVersion == "" && meta.AgentNickname == "" && meta.AgentRole == "" && meta.Source == nil && meta.Instructions == nil && meta.BaseInstructions == nil {
		return false
	}
	merged := SessionMeta{
		ID:            meta.ID,
		ForkedFromID:  meta.ForkedFromID,
		Timestamp:     meta.Timestamp,
		Cwd:           meta.Cwd,
		Git:           meta.Git,
		Originator:    meta.Originator,
		CliVersion:    meta.CliVersion,
		AgentNickname: meta.AgentNickname,
		AgentRole:     meta.AgentRole,
		Source:        meta.Source,
	}
	if meta.Instructions != nil {
		merged.Instructions = *meta.Instructions
	} else if meta.BaseInstructions != nil {
		merged.Instructions = meta.BaseInstructions.Text
	}
	applyMeta(session, merged)
	return meta.ID != "" || meta.ForkedFromID != "" || meta.Cwd != "" || meta.Timestamp != "" || meta.Git != nil
}

func prettyJSON(raw string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(raw), "", "  "); err != nil {
		return raw
	}
	return buf.String()
}

func titleForType(eventType, subType string) string {
	if eventType == "response_item" {
		switch subType {
		case "message":
			return "Message"
		case "function_call":
			return "Tool call"
		case "function_call_output":
			return "Tool output"
		case "custom_tool_call":
			return "Custom tool call"
		case "custom_tool_call_output":
			return "Custom tool output"
		case "web_search_call":
			return "Web search"
		case "ghost_snapshot":
			return "Ghost snapshot"
		case "reasoning":
			return "Reasoning"
		default:
			return "Response item"
		}
	}
	if eventType == "event_msg" {
		switch subType {
		case "user_message":
			return "User context"
		case "token_count":
			return "Token usage"
		case "task_complete":
			return "Task usage"
		}
		return "Event"
	}
	return strings.ReplaceAll(eventType, "_", " ")
}

func titleForRole(role string) string {
	switch strings.ToLower(role) {
	case "user":
		return "User"
	case "assistant":
		return "Agent"
	case "subagent":
		return "Subagent"
	default:
		return "Message"
	}
}

func roleClass(role string) string {
	switch strings.ToLower(role) {
	case "user":
		return "role-user"
	case "assistant":
		return "role-assistant"
	case "subagent":
		return "role-subagent"
	case "system":
		return "role-system"
	case "tool":
		return "role-tool"
	case "error":
		return "role-error"
	default:
		return "role-unknown"
	}
}

func mergeConsecutive(items []RenderItem) []RenderItem {
	if len(items) == 0 {
		return items
	}
	out := make([]RenderItem, 0, len(items))
	current := items[0]
	for i := 1; i < len(items); i++ {
		item := items[i]
		if current.Type == item.Type && current.Subtype == item.Subtype && current.Role == item.Role && shouldMergeConsecutive(current, item) {
			if isUserMessage(item) {
				if IsAutoContextUserMessage(current.Content) || IsAutoContextUserMessage(item.Content) {
					out = append(out, current)
					current = item
					continue
				}
				current = item
				continue
			}
			if item.EndLine > current.EndLine {
				current.EndLine = item.EndLine
			}
			if strings.TrimSpace(item.Content) != "" {
				if strings.TrimSpace(current.Content) != "" {
					current.Content = current.Content + "\n\n" + item.Content
				} else {
					current.Content = item.Content
				}
			}
			continue
		}
		out = append(out, current)
		current = item
	}
	out = append(out, current)
	return out
}

func shouldMergeConsecutive(current, item RenderItem) bool {
	if current.SubagentID != "" || item.SubagentID != "" {
		return false
	}
	switch current.Subtype {
	case "message", "reasoning":
		return true
	default:
		return false
	}
}

func trimUserRequest(content string) string {
	if !trimUserRequestEnabled {
		return content
	}
	if IsAutoContextUserMessage(content) {
		return content
	}
	marker := "## My request for Codex:"
	index := strings.Index(content, marker)
	if index == -1 {
		return content
	}
	trimmed := content[index+len(marker):]
	return strings.TrimSpace(trimmed)
}

// IsAutoContextUserMessage reports whether the content looks like auto-injected context.
func IsAutoContextUserMessage(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	if isAutoContextBlocksOnly(trimmed) {
		return true
	}
	if isLegacyCwdOnly(trimmed) {
		return true
	}
	return false
}

func maybeUpdateMetaCwd(session *Session, content string) {
	if session == nil || session.Meta == nil || session.Meta.Cwd != "" {
		return
	}
	if cwd := extractCwdFromText(content); cwd != "" {
		session.Meta.Cwd = cwd
	}
}

func extractCwdFromText(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Current working directory:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Current working directory:"))
		}
		if strings.HasPrefix(line, "CWD:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "CWD:"))
		}
	}
	return ""
}

var trimUserRequestEnabled = true

// SetTrimUserRequestEnabled controls whether user messages are trimmed to the request marker.
func SetTrimUserRequestEnabled(enabled bool) {
	trimUserRequestEnabled = enabled
}

func isUserMessage(item RenderItem) bool {
	return item.Subtype == "message" && item.Role == "user"
}

func isAgentsInstructionsOnly(text string) bool {
	remaining, ok := consumeAgentsInstructionsBlock(text)
	return ok && strings.TrimSpace(remaining) == ""
}

func isAutoContextBlocksOnly(text string) bool {
	remaining := text
	consumed := false
	for {
		remaining = strings.TrimSpace(remaining)
		if remaining == "" {
			return consumed
		}
		var ok bool
		remaining, ok = consumeAgentsInstructionsBlock(remaining)
		if ok {
			consumed = true
			continue
		}
		remaining, ok = consumeAutoContextTaggedBlock(remaining)
		if ok {
			consumed = true
			continue
		}
		return false
	}
}

func isTaggedBlocksOnly(text string) bool {
	remaining := text
	for {
		remaining = strings.TrimSpace(remaining)
		if remaining == "" {
			return true
		}
		var ok bool
		remaining, ok = consumeAutoContextTaggedBlock(remaining)
		if ok {
			continue
		}
		return false
	}
}

func consumeAgentsInstructionsBlock(text string) (string, bool) {
	if !strings.HasPrefix(text, "# AGENTS.md instructions") {
		return text, false
	}
	openIdx := strings.Index(text, "<INSTRUCTIONS>")
	if openIdx == -1 {
		return text, false
	}
	closeIdx := strings.Index(text, "</INSTRUCTIONS>")
	if closeIdx == -1 {
		return text, false
	}
	rest := text[closeIdx+len("</INSTRUCTIONS>"):]
	return rest, true
}

func consumeTaggedBlock(text, openTag, closeTag string) (string, bool) {
	if !strings.HasPrefix(text, openTag) {
		return text, false
	}
	closeIdx := strings.Index(text, closeTag)
	if closeIdx == -1 {
		return text, false
	}
	rest := text[closeIdx+len(closeTag):]
	return rest, true
}

func consumeAutoContextTaggedBlock(text string) (string, bool) {
	for _, pair := range [][2]string{
		{"<environment_context>", "</environment_context>"},
		{"<turn_aborted>", "</turn_aborted>"},
	} {
		if rest, ok := consumeTaggedBlock(text, pair[0], pair[1]); ok {
			return rest, true
		}
	}
	return text, false
}

func isLegacyCwdOnly(text string) bool {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Current working directory:") {
			continue
		}
		if strings.HasPrefix(line, "CWD:") {
			continue
		}
		return false
	}
	return true
}

func isToolWarningUserMessage(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	lines := strings.Split(trimmed, "\n")
	nonEmpty := 0
	lastNonEmpty := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		nonEmpty++
		lastNonEmpty = line
		if nonEmpty > 1 {
			return false
		}
	}
	trimmed = lastNonEmpty
	if !strings.HasPrefix(trimmed, "Warning: apply_patch was requested via ") {
		return false
	}
	if !strings.HasSuffix(trimmed, "Use the apply_patch tool instead of exec_command.") {
		return false
	}
	return true
}
