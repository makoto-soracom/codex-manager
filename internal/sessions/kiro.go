package sessions

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// KiroSidecar is the structure of a Kiro .json metadata file.
type KiroSidecar struct {
	SessionID            string            `json:"session_id"`
	Cwd                  string            `json:"cwd"`
	CreatedAt            string            `json:"created_at"`
	UpdatedAt            string            `json:"updated_at"`
	Title                string            `json:"title"`
	SessionCreatedReason string            `json:"session_created_reason"`
	SessionState         *kiroSessionState `json:"session_state,omitempty"`
}

type kiroSessionState struct {
	RTSModelState *kiroRTSModelState `json:"rts_model_state,omitempty"`
}

type kiroRTSModelState struct {
	ContextUsagePercentage float64 `json:"context_usage_percentage"`
}

// KiroContextUsage reads the context_usage_percentage from a Kiro sidecar.
// Returns the percentage and true if successfully read, or 0 and false otherwise.
func KiroContextUsage(jsonPath string) (float64, bool) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return 0, false
	}
	var sc KiroSidecar
	if err := json.Unmarshal(data, &sc); err != nil {
		return 0, false
	}
	if sc.SessionState == nil || sc.SessionState.RTSModelState == nil {
		return 0, false
	}
	return sc.SessionState.RTSModelState.ContextUsagePercentage, true
}

// ParseKiroSidecar reads a Kiro .json sidecar and returns SessionMeta.
func ParseKiroSidecar(jsonPath string) (*SessionMeta, error) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, err
	}
	var sc KiroSidecar
	if err := json.Unmarshal(data, &sc); err != nil {
		return nil, err
	}
	return &SessionMeta{
		ID:         sc.SessionID,
		Timestamp:  sc.CreatedAt,
		Cwd:        sc.Cwd,
		Originator: "kiro",
	}, nil
}

// KiroSidecarDate extracts a DateKey from the created_at field.
func KiroSidecarDate(jsonPath string) (DateKey, bool) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return DateKey{}, false
	}
	var sc KiroSidecar
	if err := json.Unmarshal(data, &sc); err != nil {
		return DateKey{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, sc.CreatedAt)
	if err != nil {
		return DateKey{}, false
	}
	return DateKey{
		Year:  fmt.Sprintf("%04d", t.Year()),
		Month: fmt.Sprintf("%02d", int(t.Month())),
		Day:   fmt.Sprintf("%02d", t.Day()),
	}, true
}

// kiro JSONL line structures

type kiroLine struct {
	Version string          `json:"version"`
	Kind    string          `json:"kind"`
	Data    json.RawMessage `json:"data"`
}

type kiroMessageData struct {
	MessageID string        `json:"message_id"`
	Content   []kiroContent `json:"content"`
	Meta      kiroMeta      `json:"meta"`
}

type kiroContent struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

type kiroMeta struct {
	Timestamp int64 `json:"timestamp"`
}

type kiroToolUse struct {
	ToolUseID string          `json:"toolUseId"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
}

type kiroToolResult struct {
	ToolUseID string        `json:"toolUseId"`
	Content   []kiroContent `json:"content"`
}

// ParseKiroSession parses a Kiro .jsonl file into a Session.
func ParseKiroSession(path string) (*Session, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	session := &Session{Path: path}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		lineText := strings.TrimSpace(scanner.Text())
		if lineText == "" {
			continue
		}

		var line kiroLine
		if err := json.Unmarshal([]byte(lineText), &line); err != nil {
			continue
		}

		items := parseKiroLine(line, lineNum)
		session.Items = append(session.Items, items...)
	}
	return session, scanner.Err()
}

func parseKiroLine(line kiroLine, lineNum int) []RenderItem {
	switch line.Kind {
	case "Prompt":
		return parseKiroPrompt(line.Data, lineNum)
	case "AssistantMessage":
		return parseKiroAssistant(line.Data, lineNum)
	case "ToolResults":
		return parseKiroToolResults(line.Data, lineNum)
	default:
		return nil
	}
}

func parseKiroPrompt(data json.RawMessage, lineNum int) []RenderItem {
	var msg kiroMessageData
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil
	}
	text := kiroExtractText(msg.Content)
	if text == "" {
		return nil
	}
	return []RenderItem{{
		Line:      lineNum,
		EndLine:   lineNum,
		Timestamp: kiroTimestamp(msg.Meta.Timestamp),
		Type:      "response_item",
		Subtype:   "message",
		Role:      "user",
		Title:     "User",
		Content:   text,
		Class:     "role-user",
	}}
}

func parseKiroAssistant(data json.RawMessage, lineNum int) []RenderItem {
	var msg kiroMessageData
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil
	}

	var items []RenderItem
	for _, c := range msg.Content {
		switch c.Kind {
		case "text":
			var text string
			if err := json.Unmarshal(c.Data, &text); err != nil {
				continue
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			items = append(items, RenderItem{
				Line:      lineNum,
				EndLine:   lineNum,
				Timestamp: kiroTimestamp(msg.Meta.Timestamp),
				Type:      "response_item",
				Subtype:   "message",
				Role:      "assistant",
				Title:     "Agent",
				Content:   text,
				Class:     "role-assistant",
			})
		case "toolUse":
			var tu kiroToolUse
			if err := json.Unmarshal(c.Data, &tu); err != nil {
				continue
			}
			content := renderKiroToolUseContent(tu)
			toolInput := ""
			// For write tool, also expose the patch as ToolInput so the
			// frontend can render it with apply_patch-style highlighting.
			if tu.Name == "write" {
				toolInput = kiroWriteToolPatch(tu.Input)
			}
			items = append(items, RenderItem{
				Line:      lineNum,
				EndLine:   lineNum,
				Timestamp: kiroTimestamp(msg.Meta.Timestamp),
				Type:      "response_item",
				Subtype:   "function_call",
				Role:      "tool",
				Title:     "Tool call: " + tu.Name,
				ToolName:  tu.Name,
				CallID:    tu.ToolUseID,
				ToolInput: toolInput,
				Content:   content,
				Class:     "role-tool",
			})
		}
	}
	return items
}

func parseKiroToolResults(data json.RawMessage, lineNum int) []RenderItem {
	var msg kiroMessageData
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil
	}

	var items []RenderItem
	for _, c := range msg.Content {
		if c.Kind != "toolResult" {
			continue
		}
		var tr kiroToolResult
		if err := json.Unmarshal(c.Data, &tr); err != nil {
			continue
		}
		output := kiroExtractText(tr.Content)
		items = append(items, RenderItem{
			Line:      lineNum,
			EndLine:   lineNum,
			Timestamp: kiroTimestamp(msg.Meta.Timestamp),
			Type:      "response_item",
			Subtype:   "function_call_output",
			Role:      "tool",
			Title:     "Tool output",
			CallID:    tr.ToolUseID,
			Content:   renderFunctionCallOutputContent(tr.ToolUseID, output),
			Class:     "role-tool",
		})
	}
	return items
}

func kiroExtractText(content []kiroContent) string {
	var parts []string
	for _, c := range content {
		if c.Kind == "text" {
			var text string
			if err := json.Unmarshal(c.Data, &text); err != nil {
				continue
			}
			if strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func kiroTimestamp(unixSec int64) string {
	if unixSec == 0 {
		return ""
	}
	return time.Unix(unixSec, 0).UTC().Format(time.RFC3339)
}

// ParseSessionForFile dispatches to the correct parser based on originator.
func ParseSessionForFile(file SessionFile) (*Session, error) {
	if file.Meta != nil && file.Meta.Originator == "kiro" {
		return ParseKiroSession(file.Path)
	}
	return ParseSession(file.Path)
}

// renderKiroToolUseContent renders a Kiro tool call as Markdown, with
// per-tool special-cased layouts for the most common tools.
func renderKiroToolUseContent(tu kiroToolUse) string {
	name := strings.TrimSpace(tu.Name)
	args := []byte(tu.Input)

	// Per-tool renderers.
	switch name {
	case "shell":
		if content := renderKiroShellTool(name, tu.ToolUseID, args); content != "" {
			return content
		}
	case "read":
		if content := renderKiroReadTool(name, tu.ToolUseID, args); content != "" {
			return content
		}
	case "write":
		if content := renderKiroWriteTool(name, tu.ToolUseID, args); content != "" {
			return content
		}
	case "grep":
		if content := renderKiroGrepTool(name, tu.ToolUseID, args); content != "" {
			return content
		}
	}

	// Fallback: generic Tool/Call ID/Arguments layout (matches Codex).
	sections := make([]string, 0, 3)
	if name != "" {
		sections = append(sections, "**Tool:** "+name)
	}
	if id := strings.TrimSpace(tu.ToolUseID); id != "" {
		sections = append(sections, "**Call ID:** "+id)
	}
	if block := renderLabeledCodeBlock("Arguments", string(args)); block != "" {
		sections = append(sections, block)
	}
	return joinMarkdownSections(sections...)
}

func renderKiroShellTool(name, callID string, args []byte) string {
	var payload struct {
		Command        string `json:"command"`
		WorkingDir     string `json:"working_dir"`
		ToolUsePurpose string `json:"__tool_use_purpose"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return ""
	}
	sections := make([]string, 0, 5)
	sections = append(sections, "**Tool:** "+name)
	if id := strings.TrimSpace(callID); id != "" {
		sections = append(sections, "**Call ID:** "+id)
	}
	if block := renderLabeledShellBlock("Command", payload.Command); block != "" {
		sections = append(sections, block)
	}
	if value := strings.TrimSpace(payload.WorkingDir); value != "" {
		sections = append(sections, "**Workdir:** `"+value+"`")
	}
	if value := strings.TrimSpace(payload.ToolUsePurpose); value != "" {
		sections = append(sections, "**Purpose:** "+value)
	}
	return joinMarkdownSections(sections...)
}

func renderKiroReadTool(name, callID string, args []byte) string {
	var payload struct {
		Operations []struct {
			Mode   string `json:"mode"`
			Path   string `json:"path"`
			Offset int    `json:"offset"`
			Limit  int    `json:"limit"`
		} `json:"operations"`
		ToolUsePurpose string `json:"__tool_use_purpose"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return ""
	}
	sections := make([]string, 0, 4)
	sections = append(sections, "**Tool:** "+name)
	if id := strings.TrimSpace(callID); id != "" {
		sections = append(sections, "**Call ID:** "+id)
	}
	if value := strings.TrimSpace(payload.ToolUsePurpose); value != "" {
		sections = append(sections, "**Purpose:** "+value)
	}
	if len(payload.Operations) > 0 {
		var lines []string
		for _, op := range payload.Operations {
			line := "- `" + op.Path + "`"
			if op.Mode != "" && op.Mode != "Line" {
				line += " (" + op.Mode + ")"
			}
			if op.Offset > 0 || op.Limit > 0 {
				line += fmt.Sprintf(" lines %d-%d", op.Offset, op.Offset+op.Limit)
			}
			lines = append(lines, line)
		}
		sections = append(sections, "**Operations**\n"+strings.Join(lines, "\n"))
	}
	return joinMarkdownSections(sections...)
}

func renderKiroWriteTool(name, callID string, args []byte) string {
	var payload struct {
		Command        string `json:"command"`
		Path           string `json:"path"`
		Content        string `json:"content"`
		OldStr         string `json:"oldStr"`
		NewStr         string `json:"newStr"`
		ToolUsePurpose string `json:"__tool_use_purpose"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return ""
	}
	sections := make([]string, 0, 6)
	sections = append(sections, "**Tool:** "+name)
	if id := strings.TrimSpace(callID); id != "" {
		sections = append(sections, "**Call ID:** "+id)
	}
	if value := strings.TrimSpace(payload.Command); value != "" {
		sections = append(sections, "**Command:** "+value)
	}
	if value := strings.TrimSpace(payload.Path); value != "" {
		sections = append(sections, "**Path:** `"+value+"`")
	}
	if value := strings.TrimSpace(payload.ToolUsePurpose); value != "" {
		sections = append(sections, "**Purpose:** "+value)
	}

	// Build a unified-ish diff representation similar to Codex's apply_patch.
	if patch := buildKiroWritePatch(payload.Command, payload.Path, payload.Content, payload.OldStr, payload.NewStr); patch != "" {
		sections = append(sections, renderLabeledDiffBlock("Patch", patch))
	}
	return joinMarkdownSections(sections...)
}

// kiroWriteToolPatch returns the apply_patch-style diff string for a Kiro write tool input.
func kiroWriteToolPatch(raw json.RawMessage) string {
	var payload struct {
		Command string `json:"command"`
		Path    string `json:"path"`
		Content string `json:"content"`
		OldStr  string `json:"oldStr"`
		NewStr  string `json:"newStr"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return buildKiroWritePatch(payload.Command, payload.Path, payload.Content, payload.OldStr, payload.NewStr)
}

// buildKiroWritePatch constructs an apply_patch-style diff from Kiro write tool args.
func buildKiroWritePatch(command, path, content, oldStr, newStr string) string {
	switch command {
	case "create":
		if content == "" {
			return ""
		}
		var b strings.Builder
		fmt.Fprintf(&b, "*** Add File: %s\n", path)
		for _, line := range strings.Split(content, "\n") {
			b.WriteString("+")
			b.WriteString(line)
			b.WriteString("\n")
		}
		return strings.TrimRight(b.String(), "\n")
	case "strReplace":
		if oldStr == "" && newStr == "" {
			return ""
		}
		var b strings.Builder
		fmt.Fprintf(&b, "*** Update File: %s\n", path)
		for _, line := range strings.Split(oldStr, "\n") {
			b.WriteString("-")
			b.WriteString(line)
			b.WriteString("\n")
		}
		for _, line := range strings.Split(newStr, "\n") {
			b.WriteString("+")
			b.WriteString(line)
			b.WriteString("\n")
		}
		return strings.TrimRight(b.String(), "\n")
	case "insert":
		if content == "" {
			return ""
		}
		var b strings.Builder
		fmt.Fprintf(&b, "*** Update File: %s\n", path)
		for _, line := range strings.Split(content, "\n") {
			b.WriteString("+")
			b.WriteString(line)
			b.WriteString("\n")
		}
		return strings.TrimRight(b.String(), "\n")
	}
	return ""
}

func renderKiroGrepTool(name, callID string, args []byte) string {
	var payload struct {
		Pattern        string `json:"pattern"`
		Path           string `json:"path"`
		Include        string `json:"include"`
		ToolUsePurpose string `json:"__tool_use_purpose"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return ""
	}
	sections := make([]string, 0, 5)
	sections = append(sections, "**Tool:** "+name)
	if id := strings.TrimSpace(callID); id != "" {
		sections = append(sections, "**Call ID:** "+id)
	}
	if value := strings.TrimSpace(payload.Pattern); value != "" {
		sections = append(sections, "**Pattern:** `"+value+"`")
	}
	if value := strings.TrimSpace(payload.Path); value != "" {
		sections = append(sections, "**Path:** `"+value+"`")
	}
	if value := strings.TrimSpace(payload.Include); value != "" {
		sections = append(sections, "**Include:** `"+value+"`")
	}
	if value := strings.TrimSpace(payload.ToolUsePurpose); value != "" {
		sections = append(sections, "**Purpose:** "+value)
	}
	return joinMarkdownSections(sections...)
}
