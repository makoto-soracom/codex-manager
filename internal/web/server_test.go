package web

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codex-manager/internal/render"
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

func TestBuildSessionViewShowsBranchAndBranchAwareResumeCommand(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join(sessionsDir, "2026", "03", "19")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionPath := filepath.Join(datePath, "branch.jsonl")
	sessionData := "" +
		"{\"timestamp\":\"2026-03-19T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-branch\",\"timestamp\":\"2026-03-19T00:00:00Z\",\"cwd\":\"/tmp/project\",\"git\":{\"branch\":\"feature/session-branch\",\"commit_hash\":\"abc123\"},\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
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
	if !strings.Contains(html, "git switch &#39;feature/session-branch&#39;") {
		t.Fatalf("expected branch-aware resume command in rendered html, got %s", html)
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
