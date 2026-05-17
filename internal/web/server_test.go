package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codex-manager/internal/render"
	"codex-manager/internal/repooverride"
	"codex-manager/internal/sessions"
)

func TestBuildSessionViewLinksSubagentNotification(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "13")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	parentPath := filepath.Join(datePath, "parent.jsonl")
	parentData := "" +
		"{\"timestamp\":\"2026-03-13T00:25:44Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"parent\",\"timestamp\":\"2026-03-13T00:25:44Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\",\"source\":\"cli\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:25:44Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"spawn_agent\",\"arguments\":\"{\\\"message\\\":\\\"review BillingStatusService\\\"}\",\"call_id\":\"call_1\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:25:44Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_1\",\"output\":\"{\\\"agent_id\\\":\\\"agent-1\\\",\\\"nickname\\\":\\\"Anscombe\\\"}\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:25:45Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"<subagent_notification>\\n{\\\"agent_id\\\":\\\"agent-1\\\",\\\"status\\\":{\\\"completed\\\":\\\"done\\\"}}\\n</subagent_notification>\"}]}}\n"
	if err := os.WriteFile(parentPath, []byte(parentData), 0o600); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	subagentPath := filepath.Join(datePath, "rollout-2026-03-13T09-23-02-agent-1.jsonl")
	subagentData := "" +
		"{\"timestamp\":\"2026-03-13T00:23:02Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"agent-1\",\"timestamp\":\"2026-03-13T00:23:02Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:23:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}}\n"
	if err := os.WriteFile(subagentPath, []byte(subagentData), 0o600); err != nil {
		t.Fatalf("write subagent: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	server := NewServer(idx, nil, nil, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "13", "parent.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	if len(view.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(view.Items))
	}
	call := view.Items[0]
	if call.Subtype != "function_call" {
		t.Fatalf("expected visible function call, got %#v", call)
	}
	if call.RoleLabel != "tool" {
		t.Fatalf("expected tool role label, got %q", call.RoleLabel)
	}
	if !strings.Contains(call.Content, "review BillingStatusService") {
		t.Fatalf("expected visible spawn request, got %q", call.Content)
	}

	spawned := view.Items[1]
	if spawned.AutoCtx {
		t.Fatalf("expected spawned subagent item to be visible")
	}
	if spawned.SubagentID != "agent-1" {
		t.Fatalf("expected spawned subagent id, got %q", spawned.SubagentID)
	}
	if spawned.SubagentStatusType != "spawned" {
		t.Fatalf("expected spawned status, got %q", spawned.SubagentStatusType)
	}
	if spawned.SubagentSessionPath != "/2026/03/13/rollout-2026-03-13T09-23-02-agent-1.jsonl#page-top" {
		t.Fatalf("unexpected spawned subagent session path: %q", spawned.SubagentSessionPath)
	}

	item := view.Items[2]
	if item.AutoCtx {
		t.Fatalf("expected subagent notification to be visible")
	}
	if item.SubagentID != "agent-1" {
		t.Fatalf("expected subagent id, got %q", item.SubagentID)
	}
	if item.SubagentNickname != "Anscombe" {
		t.Fatalf("expected subagent nickname, got %q", item.SubagentNickname)
	}
	if item.SubagentStatusType != "completed" {
		t.Fatalf("expected completed status, got %q", item.SubagentStatusType)
	}
	if item.SubagentRequest != "review BillingStatusService" {
		t.Fatalf("expected subagent request, got %q", item.SubagentRequest)
	}
	if item.SubagentSessionPath != "/2026/03/13/rollout-2026-03-13T09-23-02-agent-1.jsonl#page-top" {
		t.Fatalf("unexpected subagent session path: %q", item.SubagentSessionPath)
	}
}

func TestBuildSessionViewShowsSelectedResponseItemsAndSkipsEncryptedOnlyReasoning(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "response-items.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-1\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"web_search_call\",\"status\":\"completed\",\"action\":{\"type\":\"search\",\"query\":\"Codex CLI notify hook\",\"queries\":[\"Codex CLI notify hook\",\"OpenAI Codex notifications\"]}}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"custom_tool_call_output\",\"call_id\":\"call_patch\",\"output\":\"{\\\"output\\\":\\\"Success. Updated the following files:\\nM /tmp/file.txt\\n\\\",\\\"metadata\\\":{\\\"exit_code\\\":0}}\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"ghost_snapshot\",\"ghost_commit\":{\"id\":\"abc123\",\"parent\":\"def456\",\"preexisting_untracked_files\":[],\"preexisting_untracked_dirs\":[]}}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:04Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"reasoning\",\"summary\":[],\"content\":null,\"encrypted_content\":\"secret\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:05Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"reasoning\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"Keep this\"}]}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	server := NewServer(idx, nil, nil, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "18", "response-items.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	if len(view.Items) != 4 {
		t.Fatalf("expected 4 visible items, got %d", len(view.Items))
	}
	if view.Items[0].Subtype != "web_search_call" || !strings.Contains(view.Items[0].Content, "Expanded queries") {
		t.Fatalf("expected visible web search item, got %#v", view.Items[0])
	}
	if view.Items[1].Subtype != "custom_tool_call_output" || !strings.Contains(view.Items[1].Content, "Success. Updated the following files:") {
		t.Fatalf("expected visible custom tool output item, got %#v", view.Items[1])
	}
	if strings.Contains(string(view.Items[1].HTML), "<pre") {
		t.Fatalf("expected extracted custom tool output to avoid pre block, got %s", view.Items[1].HTML)
	}
	if view.Items[2].Subtype != "ghost_snapshot" || !strings.Contains(view.Items[2].Content, "abc123") {
		t.Fatalf("expected visible ghost snapshot item, got %#v", view.Items[2])
	}
	if view.Items[3].Subtype != "reasoning" || view.Items[3].Content != "Keep this" {
		t.Fatalf("expected only non-empty reasoning to remain, got %#v", view.Items[3])
	}
}

func TestBuildSessionViewSkipsQuerylessWebSearchCall(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "queryless-web-search.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-1\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"web_search_call\",\"status\":\"completed\",\"action\":{\"type\":\"open_page\"}}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"visible\"}]}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	server := NewServer(idx, nil, nil, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "18", "queryless-web-search.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	if len(view.Items) != 1 {
		t.Fatalf("expected only visible message item, got %d %#v", len(view.Items), view.Items)
	}
	if view.Items[0].Subtype != "message" || view.Items[0].Content != "visible" {
		t.Fatalf("expected assistant message to remain, got %#v", view.Items[0])
	}
}

func TestBuildSessionViewFormatsExecCommandToolCallSummary(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "exec-command.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-1\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"sed -n '140,170p' /tmp/README.md\\\",\\\"workdir\\\":\\\"/tmp/project\\\",\\\"max_output_tokens\\\":4000}\",\"call_id\":\"call_exec\"}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	server := NewServer(idx, nil, nil, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "18", "exec-command.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	if len(view.Items) != 1 {
		t.Fatalf("expected 1 visible item, got %d", len(view.Items))
	}
	if !strings.Contains(view.Items[0].Content, "**Workdir:** `/tmp/project`") {
		t.Fatalf("expected workdir summary, got %q", view.Items[0].Content)
	}
	if strings.Contains(view.Items[0].Content, "max_output_tokens") || strings.Contains(string(view.Items[0].HTML), "max_output_tokens") {
		t.Fatalf("expected max_output_tokens to stay hidden, content=%q html=%s", view.Items[0].Content, view.Items[0].HTML)
	}
}

func TestBuildSessionViewFormatsUpdatePlanToolCallSummary(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "update-plan.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-1\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"update_plan\",\"arguments\":\"{\\\"plan\\\":[{\\\"status\\\":\\\"completed\\\",\\\"step\\\":\\\"Add active state fields\\\"},{\\\"status\\\":\\\"in_progress\\\",\\\"step\\\":\\\"Render detailed page controls\\\"},{\\\"status\\\":\\\"pending\\\",\\\"step\\\":\\\"Run tests and update memo\\\"}]}\",\"call_id\":\"call_plan\"}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	server := NewServer(idx, nil, nil, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "18", "update-plan.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	if len(view.Items) != 1 {
		t.Fatalf("expected 1 visible item, got %d", len(view.Items))
	}
	if !strings.Contains(view.Items[0].Content, "✅ Add active state fields") || !strings.Contains(view.Items[0].Content, "□ Render detailed page controls") {
		t.Fatalf("expected update_plan summary, got %q", view.Items[0].Content)
	}
	if strings.Contains(view.Items[0].Content, "\"status\"") || strings.Contains(string(view.Items[0].HTML), "\"status\"") {
		t.Fatalf("expected raw update_plan JSON to stay hidden, content=%q html=%s", view.Items[0].Content, view.Items[0].HTML)
	}
	if !strings.Contains(string(view.Items[0].HTML), `class="update-plan-step is-in-progress"`) || !strings.Contains(string(view.Items[0].HTML), `<span class="update-plan-marker">✅</span>`) || !strings.Contains(string(view.Items[0].HTML), `class="update-plan-step is-pending"`) {
		t.Fatalf("expected plan list html, got %s", view.Items[0].HTML)
	}
}

func TestBuildSessionViewExtractsFunctionCallOutputTextPayload(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "function-call-output-text.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-1\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_text\",\"output\":\"[{\\\"text\\\":\\\"Current user is a member of 2 teams:\\\\n\\\\n<json>\\\\n{\\\\n  \\\\\\\"teams\\\\\\\": [\\\\n    {\\\\n      \\\\\\\"id\\\\\\\": \\\\\\\"team-1\\\\\\\"\\\\n    }\\\\n  ]\\\\n}\\\\n</json>\\\",\\\"type\\\":\\\"text\\\"}]\"}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	server := NewServer(idx, nil, nil, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "18", "function-call-output-text.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	if len(view.Items) != 1 {
		t.Fatalf("expected 1 visible item, got %d", len(view.Items))
	}
	if !strings.Contains(view.Items[0].Content, "Current user is a member of 2 teams:") {
		t.Fatalf("expected extracted output text, got %q", view.Items[0].Content)
	}
	if strings.Contains(view.Items[0].Content, "\"type\":\"text\"") || strings.Contains(view.Items[0].Content, "```") {
		t.Fatalf("expected raw payload to stay hidden, got %q", view.Items[0].Content)
	}
	if strings.Contains(string(view.Items[0].HTML), "<pre") {
		t.Fatalf("expected extracted function call output to avoid pre block, got %s", view.Items[0].HTML)
	}
	if !strings.Contains(string(view.Items[0].HTML), "&lt;json&gt;") || !strings.Contains(string(view.Items[0].HTML), "&lt;/json&gt;") {
		t.Fatalf("expected angle-bracket markers to stay visible as text, got %s", view.Items[0].HTML)
	}
}

func TestSessionTemplateCollapsesToolOutputByDefault(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "tool-output-collapsed.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-1\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"pwd\\\",\\\"workdir\\\":\\\"/tmp/project\\\"}\",\"call_id\":\"call_exec\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_exec\",\"output\":\"/tmp/project\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_text\",\"output\":\"plain output\"}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	renderer, err := render.New()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	server := NewServer(idx, nil, renderer, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "18", "tool-output-collapsed.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	var buf bytes.Buffer
	if err := renderer.Execute(&buf, "session", view); err != nil {
		t.Fatalf("render session: %v", err)
	}

	html := buf.String()
	if count := strings.Count(html, `<details class="tool-output-details">`); count != 2 {
		t.Fatalf("expected 2 collapsed tool output details, got %d html=%s", count, html)
	}
	if strings.Contains(html, `<details class="tool-output-details" open>`) {
		t.Fatalf("expected tool output details to stay collapsed by default, got %s", html)
	}
	if count := strings.Count(html, `Reveal output`); count != 2 {
		t.Fatalf("expected reveal output summaries for grouped and standalone outputs, got %d html=%s", count, html)
	}
}

func TestSessionTemplateFetchesMarkdownOnDemand(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "19")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "copy-markdown.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-19T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-copy\",\"timestamp\":\"2026-03-19T00:00:00Z\",\"cwd\":\"/tmp/project\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-19T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nCopy me\"}]}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	renderer, err := render.New()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	server := NewServer(idx, nil, renderer, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "19", "copy-markdown.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	var buf bytes.Buffer
	if err := renderer.Execute(&buf, "session", view); err != nil {
		t.Fatalf("render session: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, `data-copy-url="/markdown/2026/03/19/copy-markdown.jsonl"`) {
		t.Fatalf("expected thread markdown fetch url, got %s", html)
	}
	if !strings.Contains(html, `data-copy-url="/markdown/2026/03/19/copy-markdown.jsonl?line=2"`) {
		t.Fatalf("expected item markdown fetch url, got %s", html)
	}
	if strings.Contains(html, `id="md-all"`) || strings.Contains(html, `id="md-2"`) {
		t.Fatalf("expected markdown textareas to be removed, got %s", html)
	}
	if !strings.Contains(html, `scrollToElement(userSections[target], "smooth")`) || !strings.Contains(html, `setTimeout(function () { scrollToElement(target); }, 0);`) {
		t.Fatalf("expected smooth user navigation but instant initial jump, got %s", html)
	}
}

func TestSessionTemplateGroupsConsecutiveToolRunsUnderOneHeader(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "tool-run-group-header.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-group\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"pwd\\\",\\\"workdir\\\":\\\"/tmp/project\\\"}\",\"call_id\":\"call_a\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"ls\\\",\\\"workdir\\\":\\\"/tmp/project\\\"}\",\"call_id\":\"call_b\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_a\",\"output\":\"/tmp/project\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:04Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_b\",\"output\":\"file.txt\"}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	renderer, err := render.New()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	server := NewServer(idx, nil, renderer, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "18", "tool-run-group-header.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	var buf bytes.Buffer
	if err := renderer.Execute(&buf, "session", view); err != nil {
		t.Fatalf("render session: %v", err)
	}

	html := buf.String()
	if count := strings.Count(html, `class="session-group-details">`); count != 1 {
		t.Fatalf("expected 1 collapsed tool-run group details, got %d html=%s", count, html)
	}
	if strings.Contains(html, `class="session-group-details" open>`) {
		t.Fatalf("expected tool-run group to stay collapsed by default, got %s", html)
	}
	if !strings.Contains(html, "Tool run 2") {
		t.Fatalf("expected grouped tool-run title, got %s", html)
	}
	if !strings.Contains(html, `window.addEventListener("hashchange", handleInitialJump)`) || !strings.Contains(html, `openAncestorDetails(target);`) {
		t.Fatalf("expected hash jump to open collapsed tool-run groups, got %s", html)
	}
	if !strings.Contains(html, `scrollToElement(summary || lastItem);`) {
		t.Fatalf("expected default jump to keep collapsed tool-run groups closed, got %s", html)
	}
	if strings.Count(html, `class="session-header"`) != 0 {
		t.Fatalf("expected grouped tool runs to hide repeated item headers, got %s", html)
	}
	if count := strings.Count(html, `class="tool-run-part-actions"`); count != 2 {
		t.Fatalf("expected inline tool-run actions for each grouped item, got %d html=%s", count, html)
	}
	if count := strings.Count(html, `data-copy-url="/markdown/2026/03/18/tool-run-group-header.jsonl?line=`); count != 2 {
		t.Fatalf("expected 2 grouped markdown copy actions, got %d html=%s", count, html)
	}
}

func TestSessionTemplateShowsSingleToolRunGroupHeader(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "tool-run-single-group-header.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-group-1\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"pwd\\\",\\\"workdir\\\":\\\"/tmp/project\\\"}\",\"call_id\":\"call_a\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_a\",\"output\":\"/tmp/project\"}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	renderer, err := render.New()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	server := NewServer(idx, nil, renderer, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "18", "tool-run-single-group-header.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	var buf bytes.Buffer
	if err := renderer.Execute(&buf, "session", view); err != nil {
		t.Fatalf("render session: %v", err)
	}

	html := buf.String()
	if count := strings.Count(html, `class="session-group-details">`); count != 1 {
		t.Fatalf("expected 1 collapsed single tool-run group details, got %d html=%s", count, html)
	}
	if !strings.Contains(html, "Tool run 1") {
		t.Fatalf("expected single grouped tool-run title, got %s", html)
	}
	if strings.Count(html, `class="session-header"`) != 0 {
		t.Fatalf("expected single grouped tool run to hide repeated item header, got %s", html)
	}
}

func TestSessionTemplateShowsToolRunTokenUsageNearOutputAndGroupTotal(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "tool-run-token-usage.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-token-usage\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"pwd\\\",\\\"workdir\\\":\\\"/tmp/project\\\"}\",\"call_id\":\"call_a\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:02Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"input_tokens\":35041630,\"cached_input_tokens\":34567808,\"output_tokens\":27818,\"reasoning_output_tokens\":11254,\"total_tokens\":35069448},\"last_token_usage\":{\"input_tokens\":225839,\"cached_input_tokens\":225664,\"output_tokens\":37,\"reasoning_output_tokens\":0,\"total_tokens\":225876},\"model_context_window\":258400},\"rate_limits\":{\"limit_id\":\"codex\",\"limit_name\":null,\"primary\":null,\"secondary\":null,\"credits\":null,\"plan_type\":\"team\",\"rate_limit_reached_type\":null}}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_a\",\"output\":\"/tmp/project\"}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	renderer, err := render.New()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	server := NewServer(idx, nil, renderer, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "18", "tool-run-token-usage.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}
	if len(view.Items) != 1 {
		t.Fatalf("expected token usage to be folded into one grouped item, got %d items: %#v", len(view.Items), view.Items)
	}
	wantUsage := "input=175, cached=225,664, output=37 (reasoning=0), total=225,876"
	if view.Items[0].ToolRunUsage != wantUsage {
		t.Fatalf("expected output token usage %q, got %q", wantUsage, view.Items[0].ToolRunUsage)
	}
	if view.Items[0].ToolRunGroupUsage != wantUsage {
		t.Fatalf("expected grouped token usage %q, got %q", wantUsage, view.Items[0].ToolRunGroupUsage)
	}

	var buf bytes.Buffer
	if err := renderer.Execute(&buf, "session", view); err != nil {
		t.Fatalf("render session: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Tool run 1") || !strings.Contains(html, "Lines 2-4") || !strings.Contains(html, wantUsage) {
		t.Fatalf("expected grouped and output token usage, got %s", html)
	}
	if strings.Contains(html, "Token usage") {
		t.Fatalf("expected no standalone token usage block, got %s", html)
	}
}

func TestSessionTemplateShowsSharedToolRunTokenUsageOnce(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "tool-run-shared-token-usage.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-shared-token-usage\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"one\\\"}\",\"call_id\":\"call_a\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"two\\\"}\",\"call_id\":\"call_b\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"three\\\"}\",\"call_id\":\"call_c\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:04Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"four\\\"}\",\"call_id\":\"call_d\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:05Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"input_tokens\":565060,\"cached_input_tokens\":451072,\"output_tokens\":4097,\"reasoning_output_tokens\":1658,\"total_tokens\":569157},\"last_token_usage\":{\"input_tokens\":94650,\"cached_input_tokens\":91520,\"output_tokens\":449,\"reasoning_output_tokens\":56,\"total_tokens\":95099},\"model_context_window\":258400},\"rate_limits\":{\"limit_id\":\"codex\",\"limit_name\":null,\"primary\":null,\"secondary\":null,\"credits\":null,\"plan_type\":\"team\",\"rate_limit_reached_type\":null}}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:06Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_a\",\"output\":\"one\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:07Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_b\",\"output\":\"two\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:08Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_c\",\"output\":\"three\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:09Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_d\",\"output\":\"four\"}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	renderer, err := render.New()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	server := NewServer(idx, nil, renderer, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "18", "tool-run-shared-token-usage.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}
	if len(view.Items) != 4 {
		t.Fatalf("expected four grouped tool run items, got %d items: %#v", len(view.Items), view.Items)
	}
	wantUsage := "input=3,130, cached=91,520, output=449 (reasoning=56), total=95,099"
	if view.Items[0].ToolRunUsage != wantUsage {
		t.Fatalf("expected output token usage %q, got %q", wantUsage, view.Items[0].ToolRunUsage)
	}
	if view.Items[0].ToolRunGroupUsage != wantUsage {
		t.Fatalf("expected grouped token usage %q, got %q", wantUsage, view.Items[0].ToolRunGroupUsage)
	}
	for index, item := range view.Items[1:] {
		if item.ToolRunUsage != "" {
			t.Fatalf("expected shared token usage to be attached only once, item %d has %q", index+1, item.ToolRunUsage)
		}
	}

	var buf bytes.Buffer
	if err := renderer.Execute(&buf, "session", view); err != nil {
		t.Fatalf("render session: %v", err)
	}
	html := buf.String()
	if count := strings.Count(html, wantUsage); count != 2 {
		t.Fatalf("expected shared token usage in group header and one output, got %d html=%s", count, html)
	}
}

func TestSessionTemplateFoldsFinalAnswerUsageAndShowsTaskSummary(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "final-answer-token-usage.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-final-usage\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:01Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"input_tokens\":1000,\"cached_input_tokens\":512,\"output_tokens\":10,\"reasoning_output_tokens\":0,\"total_tokens\":1010},\"last_token_usage\":{\"input_tokens\":1000,\"cached_input_tokens\":512,\"output_tokens\":10,\"reasoning_output_tokens\":0,\"total_tokens\":1010},\"model_context_window\":258400},\"rate_limits\":{\"limit_id\":\"codex\",\"plan_type\":\"team\"}}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:02Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"phase\":\"final_answer\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:04Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"input_tokens\":2200,\"cached_input_tokens\":1536,\"output_tokens\":50,\"reasoning_output_tokens\":0,\"total_tokens\":2250},\"last_token_usage\":{\"input_tokens\":1200,\"cached_input_tokens\":1024,\"output_tokens\":40,\"reasoning_output_tokens\":0,\"total_tokens\":1240},\"model_context_window\":258400},\"rate_limits\":{\"limit_id\":\"codex\",\"plan_type\":\"team\"}}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:05Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\",\"duration_ms\":102675}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	renderer, err := render.New()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	server := NewServer(idx, nil, renderer, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "18", "final-answer-token-usage.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}
	if len(view.Items) != 3 {
		t.Fatalf("expected baseline token, assistant message, and task summary, got %d items: %#v", len(view.Items), view.Items)
	}
	wantUsage := "input=176, cached=1,024, output=40 (reasoning=0), total=1,240"
	if view.Items[1].ResponseUsage != wantUsage {
		t.Fatalf("expected final answer usage %q, got %q", wantUsage, view.Items[1].ResponseUsage)
	}
	wantSummary := "Worked for 1m 42s (" + wantUsage + ")"
	if view.Items[2].Subtype != "task_complete" || view.Items[2].Content != wantSummary {
		t.Fatalf("expected task summary %q, got %#v", wantSummary, view.Items[2])
	}

	var buf bytes.Buffer
	if err := renderer.Execute(&buf, "session", view); err != nil {
		t.Fatalf("render session: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `class="meta response-usage"`) || !strings.Contains(html, wantUsage) || !strings.Contains(html, wantSummary) {
		t.Fatalf("expected final answer usage and task summary in html, got %s", html)
	}
	if strings.Contains(html, "Line 5") {
		t.Fatalf("expected folded token_count line to be hidden, got %s", html)
	}
}

func TestHandleSessionMarkdownReturnsThreadAndGroupedLineMarkdown(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "markdown.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-md\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"pwd\\\",\\\"workdir\\\":\\\"/tmp/project\\\"}\",\"call_id\":\"call_exec\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_exec\",\"output\":\"/tmp/project\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	server := NewServer(idx, nil, nil, sessionsDir, "", "", 3)

	req := httptest.NewRequest(http.MethodGet, "/markdown/2026/03/18/markdown.jsonl", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for full markdown, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "## Tool run") || !strings.Contains(body, "### Tool call") || !strings.Contains(body, "### Tool output") || !strings.Contains(body, "## Agent") {
		t.Fatalf("expected grouped thread markdown, got %q", body)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", got)
	}

	lineReq := httptest.NewRequest(http.MethodGet, "/markdown/2026/03/18/markdown.jsonl?line=3", nil)
	lineRec := httptest.NewRecorder()
	server.ServeHTTP(lineRec, lineReq)

	if lineRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for grouped line markdown, got %d body=%s", lineRec.Code, lineRec.Body.String())
	}
	lineBody := lineRec.Body.String()
	if !strings.Contains(lineBody, "## Tool run") || !strings.Contains(lineBody, "### Tool output") || strings.Contains(lineBody, "## Agent") {
		t.Fatalf("expected grouped markdown for output line only, got %q", lineBody)
	}
}

func TestBuildSessionViewShowsBranchAndBranchAwareResumeCommand(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "19")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "branch.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-19T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-branch\",\"timestamp\":\"2026-03-19T00:00:00Z\",\"cwd\":\"/tmp/project\",\"git\":{\"branch\":\"feature/session-branch\",\"commit_hash\":\"abc123\",\"repository_url\":\"https://github.com/cinkster/codex-manager.git\"},\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-19T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nShow branch\"}]}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	renderer, err := render.New()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	server := NewServer(idx, nil, renderer, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "19", "branch.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	if view.File.Branch != "feature/session-branch" {
		t.Fatalf("expected branch on session view, got %q", view.File.Branch)
	}
	if view.File.BranchURL != "https://github.com/cinkster/codex-manager/tree/feature/session-branch" {
		t.Fatalf("expected branch url on session view, got %q", view.File.BranchURL)
	}
	if view.ResumeCommand != "cd '/tmp/project'\ngit switch 'feature/session-branch'\ncodex resume session-branch" {
		t.Fatalf("unexpected resume command: %q", view.ResumeCommand)
	}

	var buf bytes.Buffer
	if err := renderer.Execute(&buf, "session", view); err != nil {
		t.Fatalf("render session: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Branch: feature/session-branch") {
		t.Fatalf("expected branch label in rendered html, got %s", html)
	}
	if !strings.Contains(html, `href="https://github.com/cinkster/codex-manager/tree/feature/session-branch"`) {
		t.Fatalf("expected github branch link in rendered html, got %s", html)
	}
	if !strings.Contains(html, `target="_blank"`) {
		t.Fatalf("expected branch link to open in new window, got %s", html)
	}
	if !strings.Contains(html, `class="session-page-branch-icon-svg"`) {
		t.Fatalf("expected branch icon svg in rendered html, got %s", html)
	}
	if !strings.Contains(html, "git switch &#39;feature/session-branch&#39;") {
		t.Fatalf("expected branch-aware resume command in rendered html, got %s", html)
	}
}

func TestBuildSessionViewUsesRepositoryOverrideForBranchURL(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "19")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "branch-override.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-19T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-branch-override\",\"timestamp\":\"2026-03-19T00:00:00Z\",\"cwd\":\"/home/makoto/codex-manager/internal/web\",\"git\":{\"branch\":\"feature/session-branch\",\"commit_hash\":\"abc123\",\"repository_url\":\"https://github.com/cinkster/codex-manager.git\"},\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-19T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nShow branch\"}]}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	overridePath := filepath.Join(t.TempDir(), "session_repository_overrides.json")
	overrideData := `{
  "version": 1,
  "rules": [
    {
      "cwd_prefix": "/home/makoto/codex-manager",
      "repository_url": "https://github.com/makoto-soracom/codex-manager.git"
    }
  ]
}
`
	if err := os.WriteFile(overridePath, []byte(overrideData), 0o600); err != nil {
		t.Fatalf("write overrides: %v", err)
	}

	overrideStore, err := repooverride.LoadStore(overridePath)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	server := NewServer(idx, nil, nil, sessionsDir, "", "", 3)
	server.EnableRepoOverrides(overrideStore)
	view, err := server.buildSessionView([]string{"2026", "03", "19", "branch-override.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	if view.File.BranchURL != "https://github.com/makoto-soracom/codex-manager/tree/feature/session-branch" {
		t.Fatalf("expected override branch url on session view, got %q", view.File.BranchURL)
	}
}

func TestBuildSessionViewOmitsSiblingSessionNavAndKeepsUserJumpControls(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "19")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	olderPath := filepath.Join(datePath, "older.jsonl")
	olderData := "" +
		"{\"timestamp\":\"2026-03-19T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-older\",\"timestamp\":\"2026-03-19T00:00:00Z\",\"cwd\":\"/tmp/project\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-19T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nOlder\"}]}}\n"
	if err := os.WriteFile(olderPath, []byte(olderData), 0o600); err != nil {
		t.Fatalf("write older session: %v", err)
	}

	currentPath := filepath.Join(datePath, "current.jsonl")
	currentData := "" +
		"{\"timestamp\":\"2026-03-19T00:10:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-current\",\"timestamp\":\"2026-03-19T00:10:00Z\",\"cwd\":\"/tmp/project\",\"git\":{\"branch\":\"feature/session-branch\",\"commit_hash\":\"abc123\"},\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-19T00:10:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nCurrent\"}]}}\n"
	if err := os.WriteFile(currentPath, []byte(currentData), 0o600); err != nil {
		t.Fatalf("write current session: %v", err)
	}

	newerPath := filepath.Join(datePath, "newer.jsonl")
	newerData := "" +
		"{\"timestamp\":\"2026-03-19T00:20:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-newer\",\"timestamp\":\"2026-03-19T00:20:00Z\",\"cwd\":\"/tmp/project\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-19T00:20:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nNewer\"}]}}\n"
	if err := os.WriteFile(newerPath, []byte(newerData), 0o600); err != nil {
		t.Fatalf("write newer session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	renderer, err := render.New()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	server := NewServer(idx, nil, renderer, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "19", "current.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	var buf bytes.Buffer
	if err := renderer.Execute(&buf, "session", view); err != nil {
		t.Fatalf("render session: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, "CWD: /tmp/project") {
		t.Fatalf("expected cwd in toolbar, got %s", html)
	}
	if !strings.Contains(html, "Branch: feature/session-branch") {
		t.Fatalf("expected branch in toolbar, got %s", html)
	}
	if !strings.Contains(html, "Previous user message") || !strings.Contains(html, "Next user message") || !strings.Contains(html, "Last user message") {
		t.Fatalf("expected user jump controls in toolbar, got %s", html)
	}
	if strings.Contains(html, "/2026/03/19/older.jsonl#last-user") || strings.Contains(html, "/2026/03/19/newer.jsonl#last-user") {
		t.Fatalf("expected sibling session navigation links to be removed, got %s", html)
	}
}

func TestBuildSessionViewRendersApplyPatchAsPatchBlock(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "apply-patch.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-1\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"custom_tool_call\",\"status\":\"completed\",\"call_id\":\"call_patch\",\"name\":\"apply_patch\",\"input\":\"*** Begin Patch\\n*** Update File: /tmp/file.txt\\n@@\\n-old\\n+new\\n*** End Patch\"}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	server := NewServer(idx, nil, nil, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "18", "apply-patch.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	if len(view.Items) != 1 {
		t.Fatalf("expected 1 visible item, got %d", len(view.Items))
	}
	html := string(view.Items[0].HTML)
	if !strings.Contains(html, `class="patch-block"`) {
		t.Fatalf("expected patch block html, got %s", html)
	}
	if !strings.Contains(html, `patch-line-add">+new`) || !strings.Contains(html, `patch-line-del">-old`) {
		t.Fatalf("expected add/delete patch lines, got %s", html)
	}
	if strings.Contains(html, "<pre") {
		t.Fatalf("expected apply_patch html to avoid generic pre block, got %s", html)
	}
}

func TestBuildSessionViewGroupsAdjacentToolCallAndOutputByCallID(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "tool-run.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-1\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"pwd\\\",\\\"workdir\\\":\\\"/tmp/project\\\"}\",\"call_id\":\"call_exec\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_exec\",\"output\":\"/tmp/project\"}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	server := NewServer(idx, nil, nil, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "18", "tool-run.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	if len(view.Items) != 1 {
		t.Fatalf("expected 1 grouped item, got %d", len(view.Items))
	}
	item := view.Items[0]
	if item.Title != "Tool run" || item.Subtype != "tool_run" {
		t.Fatalf("expected grouped tool run item, got %#v", item)
	}
	if item.ToolRunGroupTitle != "Tool run 1" {
		t.Fatalf("expected grouped header title on single item, got %q", item.ToolRunGroupTitle)
	}
	if !item.ToolRunHideHeader {
		t.Fatalf("expected single grouped tool run header to be hidden")
	}
	if !item.ToolRunGroupEnd {
		t.Fatalf("expected single grouped tool run to close group")
	}
	if item.ToolRunOutputLine != 3 {
		t.Fatalf("expected grouped output line 3, got %d", item.ToolRunOutputLine)
	}
	if !strings.Contains(string(item.HTML), "pwd") {
		t.Fatalf("expected call HTML to contain command, got %s", item.HTML)
	}
	if !strings.Contains(string(item.ToolRunOutputHTML), "/tmp/project") {
		t.Fatalf("expected output HTML to contain command output, got %s", item.ToolRunOutputHTML)
	}
	if !strings.Contains(item.Markdown, "### Tool call") || !strings.Contains(item.Markdown, "### Tool output") {
		t.Fatalf("expected grouped markdown sections, got %q", item.Markdown)
	}
}

func TestBuildSessionViewGroupsNonAdjacentToolCallAndOutputByCallID(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "tool-run-non-adjacent.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-1\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"pwd\\\",\\\"workdir\\\":\\\"/tmp/project\\\"}\",\"call_id\":\"call_a\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"ls\\\",\\\"workdir\\\":\\\"/tmp/project\\\"}\",\"call_id\":\"call_b\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_a\",\"output\":\"/tmp/project\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:00:04Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_b\",\"output\":\"file.txt\"}}\n"
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	server := NewServer(idx, nil, nil, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "18", "tool-run-non-adjacent.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	if len(view.Items) != 2 {
		t.Fatalf("expected 2 grouped items, got %d", len(view.Items))
	}
	first := view.Items[0]
	if first.Title != "Tool run" || first.Subtype != "tool_run" {
		t.Fatalf("expected first grouped tool run item, got %#v", first)
	}
	if first.ToolRunGroupTitle != "Tool run 2" {
		t.Fatalf("expected grouped header title on first item, got %q", first.ToolRunGroupTitle)
	}
	if !first.ToolRunHideHeader {
		t.Fatalf("expected first grouped tool run header to be hidden")
	}
	if first.ToolRunGroupLastLine != 5 {
		t.Fatalf("expected first grouped tool run last line 5, got %d", first.ToolRunGroupLastLine)
	}
	if first.ToolRunGroupEnd {
		t.Fatalf("expected first grouped tool run not to close group")
	}
	if first.ToolRunOutputLine != 4 {
		t.Fatalf("expected first grouped output line 4, got %d", first.ToolRunOutputLine)
	}
	if !strings.Contains(string(first.HTML), "pwd") || !strings.Contains(string(first.ToolRunOutputHTML), "/tmp/project") {
		t.Fatalf("expected first grouped call/output, got call=%s output=%s", first.HTML, first.ToolRunOutputHTML)
	}

	second := view.Items[1]
	if second.Title != "Tool run" || second.Subtype != "tool_run" {
		t.Fatalf("expected second grouped tool run item, got %#v", second)
	}
	if second.ToolRunGroupTitle != "" {
		t.Fatalf("expected only first grouped item to carry group title, got %q", second.ToolRunGroupTitle)
	}
	if !second.ToolRunHideHeader {
		t.Fatalf("expected second grouped tool run header to be hidden")
	}
	if !second.ToolRunGroupEnd {
		t.Fatalf("expected second grouped tool run to close group")
	}
	if second.ToolRunOutputLine != 5 {
		t.Fatalf("expected second grouped output line 5, got %d", second.ToolRunOutputLine)
	}
	if !strings.Contains(string(second.HTML), "ls") || !strings.Contains(string(second.ToolRunOutputHTML), "file.txt") {
		t.Fatalf("expected second grouped call/output, got call=%s output=%s", second.HTML, second.ToolRunOutputHTML)
	}
}

func TestBuildSessionViewLabelsSubagentThreadConversation(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "13")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	parentPath := filepath.Join(datePath, "parent.jsonl")
	parentData := "" +
		"{\"timestamp\":\"2026-03-13T00:15:36Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"parent-1\",\"timestamp\":\"2026-03-13T00:15:36Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\",\"source\":\"cli\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:15:37Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"spawning\"}]}}\n"
	if err := os.WriteFile(parentPath, []byte(parentData), 0o600); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	subagentPath := filepath.Join(datePath, "subagent.jsonl")
	subagentData := "" +
		"{\"timestamp\":\"2026-03-13T00:23:02Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"agent-1\",\"forked_from_id\":\"parent-1\",\"timestamp\":\"2026-03-13T00:23:02Z\",\"cwd\":\"/tmp\",\"originator\":\"codex_cli_rs\",\"cli_version\":\"0.114.0\",\"source\":{\"subagent\":{\"thread_spawn\":{\"parent_thread_id\":\"parent-1\",\"depth\":1,\"agent_nickname\":\"Anscombe\",\"agent_role\":\"explorer\"}}},\"agent_nickname\":\"Anscombe\",\"agent_role\":\"explorer\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:23:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"I checked the code.\"}]}}\n" +
		"{\"timestamp\":\"2026-03-13T00:23:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"<environment_context>\\nCurrent working directory: /tmp\\n</environment_context>\"}]}}\n" +
		"{\"timestamp\":\"2026-03-13T00:23:04Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"Please inspect the parser.\"}]}}\n"
	if err := os.WriteFile(subagentPath, []byte(subagentData), 0o600); err != nil {
		t.Fatalf("write subagent: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	server := NewServer(idx, nil, nil, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "13", "subagent.jsonl"})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	if !view.IsSubagentThread {
		t.Fatalf("expected subagent thread view")
	}
	if view.SubagentDisplayName != "Anscombe" {
		t.Fatalf("expected subagent display name, got %q", view.SubagentDisplayName)
	}
	if view.SubagentDisplayRole != "explorer" {
		t.Fatalf("expected subagent display role, got %q", view.SubagentDisplayRole)
	}
	if view.ParentSessionPath != "/2026/03/13/parent.jsonl#page-top" {
		t.Fatalf("unexpected parent session path: %q", view.ParentSessionPath)
	}
	if view.UserNavLabel != "agent" {
		t.Fatalf("expected agent nav label, got %q", view.UserNavLabel)
	}
	if len(view.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(view.Items))
	}

	subagentItem := view.Items[0]
	if subagentItem.Title != "Subagent" {
		t.Fatalf("expected subagent title, got %q", subagentItem.Title)
	}
	if subagentItem.RoleLabel != "subagent" {
		t.Fatalf("expected subagent role label, got %q", subagentItem.RoleLabel)
	}
	if subagentItem.SpeakerClass != "subagent" {
		t.Fatalf("expected subagent speaker class, got %q", subagentItem.SpeakerClass)
	}
	if subagentItem.SpeakerName != "Anscombe" {
		t.Fatalf("expected subagent speaker name, got %q", subagentItem.SpeakerName)
	}
	if subagentItem.SpeakerRole != "explorer" {
		t.Fatalf("expected subagent speaker role, got %q", subagentItem.SpeakerRole)
	}

	autoContextItem := view.Items[1]
	if !autoContextItem.AutoCtx {
		t.Fatalf("expected auto context item")
	}
	if autoContextItem.Title != "Agent" {
		t.Fatalf("expected auto context title to be Agent, got %q", autoContextItem.Title)
	}
	if autoContextItem.RoleLabel != "agent" {
		t.Fatalf("expected auto context role label, got %q", autoContextItem.RoleLabel)
	}
	if autoContextItem.SpeakerClass != "agent" {
		t.Fatalf("expected auto context speaker class, got %q", autoContextItem.SpeakerClass)
	}

	agentItem := view.Items[2]
	if agentItem.Title != "Agent" {
		t.Fatalf("expected agent title, got %q", agentItem.Title)
	}
	if agentItem.RoleLabel != "agent" {
		t.Fatalf("expected agent role label, got %q", agentItem.RoleLabel)
	}
	if agentItem.SpeakerClass != "agent" {
		t.Fatalf("expected agent speaker class, got %q", agentItem.SpeakerClass)
	}
}

func TestBuildSessionViewsWithSnippetsUsesSemanticSpeakerLabels(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "13")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	standardPath := filepath.Join(datePath, "standard.jsonl")
	standardData := "" +
		"{\"timestamp\":\"2026-03-13T00:10:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"standard-1\",\"timestamp\":\"2026-03-13T00:10:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:10:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"Please check the list view.\"}]}}\n" +
		"{\"timestamp\":\"2026-03-13T00:10:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"I updated the session snippet.\"}]}}\n"
	if err := os.WriteFile(standardPath, []byte(standardData), 0o600); err != nil {
		t.Fatalf("write standard session: %v", err)
	}

	subagentPath := filepath.Join(datePath, "subagent.jsonl")
	subagentData := "" +
		"{\"timestamp\":\"2026-03-13T00:23:02Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"agent-1\",\"forked_from_id\":\"parent-1\",\"timestamp\":\"2026-03-13T00:23:02Z\",\"cwd\":\"/tmp\",\"originator\":\"codex_cli_rs\",\"cli_version\":\"0.114.0\",\"source\":{\"subagent\":{\"thread_spawn\":{\"parent_thread_id\":\"parent-1\",\"depth\":1,\"agent_nickname\":\"Anscombe\",\"agent_role\":\"explorer\"}}},\"agent_nickname\":\"Anscombe\",\"agent_role\":\"explorer\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:23:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"I checked the code.\"}]}}\n" +
		"{\"timestamp\":\"2026-03-13T00:23:04Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"Please inspect the parser.\"}]}}\n"
	if err := os.WriteFile(subagentPath, []byte(subagentData), 0o600); err != nil {
		t.Fatalf("write subagent session: %v", err)
	}

	info, err := os.Stat(standardPath)
	if err != nil {
		t.Fatalf("stat standard session: %v", err)
	}
	subagentInfo, err := os.Stat(subagentPath)
	if err != nil {
		t.Fatalf("stat subagent session: %v", err)
	}
	files := []sessions.SessionFile{
		{
			Name:    "standard.jsonl",
			Path:    standardPath,
			Date:    sessions.DateKey{Year: "2026", Month: "03", Day: "13"},
			ModTime: info.ModTime(),
			Size:    info.Size(),
		},
		{
			Name:    "subagent.jsonl",
			Path:    subagentPath,
			Date:    sessions.DateKey{Year: "2026", Month: "03", Day: "13"},
			ModTime: subagentInfo.ModTime(),
			Size:    subagentInfo.Size(),
		},
	}

	server := NewServer(nil, nil, nil, "", "", "", 3)
	views := server.buildSessionViewsWithSnippets(files)
	if len(views) != 2 {
		t.Fatalf("expected 2 views, got %d", len(views))
	}

	standardView := views[0]
	if standardView.LastUserSnippetTitle != "User" {
		t.Fatalf("expected standard user title, got %q", standardView.LastUserSnippetTitle)
	}
	if standardView.LastUserSnippetClass != "user" {
		t.Fatalf("expected standard user class, got %q", standardView.LastUserSnippetClass)
	}
	if standardView.LastAssistantSnippetTitle != "Agent" {
		t.Fatalf("expected standard assistant title, got %q", standardView.LastAssistantSnippetTitle)
	}
	if standardView.LastAssistantSnippetClass != "agent" {
		t.Fatalf("expected standard assistant class, got %q", standardView.LastAssistantSnippetClass)
	}

	subagentView := views[1]
	if subagentView.LastUserSnippetTitle != "Agent" {
		t.Fatalf("expected subagent user title to be Agent, got %q", subagentView.LastUserSnippetTitle)
	}
	if subagentView.LastUserSnippetClass != "agent" {
		t.Fatalf("expected subagent user class to be agent, got %q", subagentView.LastUserSnippetClass)
	}
	if subagentView.LastAssistantSnippetTitle != "Subagent" {
		t.Fatalf("expected subagent assistant title to be Subagent, got %q", subagentView.LastAssistantSnippetTitle)
	}
	if subagentView.LastAssistantSnippetClass != "subagent" {
		t.Fatalf("expected subagent assistant class to be subagent, got %q", subagentView.LastAssistantSnippetClass)
	}
}

func TestBuildSessionViewsUseDisplayNameFromSessionIndex(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	datePath := filepath.Join(sessionsDir, "2026", "03", "13")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "session_index.jsonl"), []byte("{\"id\":\"session-1\",\"thread_name\":\"pr461 #3\",\"updated_at\":\"2026-03-13T06:09:42Z\"}\n"), 0o600); err != nil {
		t.Fatalf("write session index: %v", err)
	}

	fileName := "rollout-2026-03-13T13-36-02-session-1.jsonl"
	filePath := filepath.Join(datePath, fileName)
	data := "" +
		"{\"timestamp\":\"2026-03-13T00:25:44Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-1\",\"timestamp\":\"2026-03-13T00:25:44Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\",\"source\":\"cli\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:25:45Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"Please check the display name.\"}]}}\n"
	if err := os.WriteFile(filePath, []byte(data), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	server := NewServer(idx, nil, nil, sessionsDir, "", "", 3)
	view, err := server.buildSessionView([]string{"2026", "03", "13", fileName})
	if err != nil {
		t.Fatalf("buildSessionView: %v", err)
	}

	want := "pr461 #3 (" + fileName + ")"
	if view.File.DisplayName != want {
		t.Fatalf("expected display name %q, got %q", want, view.File.DisplayName)
	}

	date, ok := sessions.ParseDate("2026", "03", "13")
	if !ok {
		t.Fatal("expected valid date")
	}
	listViews := server.buildSessionViewsWithSnippets(idx.SessionsByDate(date))
	if len(listViews) != 1 {
		t.Fatalf("expected 1 list view, got %d", len(listViews))
	}
	if listViews[0].DisplayName != want {
		t.Fatalf("expected list display name %q, got %q", want, listViews[0].DisplayName)
	}
}
