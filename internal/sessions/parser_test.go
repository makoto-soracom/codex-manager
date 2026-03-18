package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSession(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "session.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-01-09T01:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"abc\",\"timestamp\":\"2026-01-09T01:00:00Z\",\"cwd\":\"/tmp\",\"git\":{\"branch\":\"feature/test-branch\",\"commit_hash\":\"abc123\"},\"originator\":\"cli\",\"cli_version\":\"0.1\",\"instructions\":\"hello\"}}\n" +
		"{\"timestamp\":\"2026-01-09T01:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"Context that should be dropped\"}]}}\n" +
		"{\"timestamp\":\"2026-01-09T01:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"Hello\\n\\n## My request for Codex:\\nOnly this\"}]}}\n" +
		"{\"timestamp\":\"2026-01-09T01:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"shell_command\",\"arguments\":\"{}\",\"call_id\":\"call_1\"}}\n" +
		"{\"timestamp\":\"2026-01-09T01:00:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_1\",\"output\":\"done\"}}\n" +
		"{\"timestamp\":\"2026-01-09T01:00:04Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"reasoning\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"Reason\"}]}}\n" +
		"{\"timestamp\":\"2026-01-09T01:00:05Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"Earlier\"}]}}\n" +
		"{\"timestamp\":\"2026-01-09T01:00:06Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"Later\"}]}}\n" +
		"{\"timestamp\":\"2026-01-09T01:00:05Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"Context\",\"images\":[]}}\n" +
		"not-json\n"

	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	session, err := ParseSession(filePath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if session.Meta == nil || session.Meta.ID != "abc" {
		t.Fatalf("expected session meta")
	}
	if session.Meta.GitBranch() != "feature/test-branch" {
		t.Fatalf("expected git branch, got %q", session.Meta.GitBranch())
	}
	if len(session.Items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(session.Items))
	}
	if session.Items[0].Content != "Only this" {
		t.Fatalf("unexpected message content: %q", session.Items[0].Content)
	}
	if session.Items[1].Subtype != "function_call" || !strings.Contains(session.Items[1].Content, "**Tool:** shell_command") {
		t.Fatalf("expected visible tool call, got %#v", session.Items[1])
	}
	if session.Items[2].Subtype != "function_call_output" || !strings.Contains(session.Items[2].Content, "**Output**") {
		t.Fatalf("expected visible tool output, got %#v", session.Items[2])
	}
	if session.Items[3].Content != "Reason" {
		t.Fatalf("expected reasoning summary, got %q", session.Items[3].Content)
	}
	if session.Items[4].Content != "Later" {
		t.Fatalf("expected last user message, got %q", session.Items[4].Content)
	}
}

func TestParseSessionDirectFormat(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "session.jsonl")
	data := "" +
		"{\"id\":\"xyz\",\"timestamp\":\"2025-08-27T16:17:00.964Z\",\"instructions\":null}\n" +
		"{\"record_type\":\"state\"}\n" +
		"{\"type\":\"message\",\"id\":null,\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"<environment_context>\\nCurrent working directory: /tmp\\n</environment_context>\"}]}\n" +
		"{\"type\":\"message\",\"id\":null,\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Hello\"}]}\n" +
		"{\"type\":\"reasoning\",\"id\":\"rs_1\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"Reason\"}]}\n"

	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	session, err := ParseSession(filePath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if session.Meta == nil || session.Meta.ID != "xyz" {
		t.Fatalf("expected session meta id, got %#v", session.Meta)
	}
	if session.Meta.Cwd != "/tmp" {
		t.Fatalf("expected cwd /tmp, got %q", session.Meta.Cwd)
	}
	if len(session.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(session.Items))
	}
	if session.Items[1].Content != "Hello" {
		t.Fatalf("unexpected assistant content: %q", session.Items[1].Content)
	}
	if session.Items[2].Content != "Reason" {
		t.Fatalf("unexpected reasoning content: %q", session.Items[2].Content)
	}
}

func TestParseSessionCapturesSubagentThreadMeta(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "session.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-13T00:23:02Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"agent-1\",\"forked_from_id\":\"parent-1\",\"timestamp\":\"2026-03-13T00:23:02Z\",\"cwd\":\"/tmp\",\"originator\":\"codex_cli_rs\",\"cli_version\":\"0.114.0\",\"source\":{\"subagent\":{\"thread_spawn\":{\"parent_thread_id\":\"parent-1\",\"depth\":1,\"agent_nickname\":\"Anscombe\",\"agent_role\":\"explorer\"}}},\"agent_nickname\":\"Anscombe\",\"agent_role\":\"explorer\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:23:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}}\n"

	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	session, err := ParseSession(filePath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if session.Meta == nil {
		t.Fatalf("expected session meta")
	}
	if !session.Meta.IsSubagentThread() {
		t.Fatalf("expected subagent thread meta, got %#v", session.Meta)
	}
	if session.Meta.ParentThreadID() != "parent-1" {
		t.Fatalf("expected parent thread id, got %q", session.Meta.ParentThreadID())
	}
	if session.Meta.SubagentNicknameValue() != "Anscombe" {
		t.Fatalf("expected subagent nickname, got %q", session.Meta.SubagentNicknameValue())
	}
	if session.Meta.SubagentRoleValue() != "explorer" {
		t.Fatalf("expected subagent role, got %q", session.Meta.SubagentRoleValue())
	}
}

func TestParseSessionPreservesAutoContextBeforeUserRequest(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "session.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-09T01:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"abc\",\"timestamp\":\"2026-03-09T01:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\",\"base_instructions\":{\"text\":\"base\"}}}\n" +
		"{\"timestamp\":\"2026-03-09T01:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"developer\",\"content\":[{\"type\":\"input_text\",\"text\":\"ignored\"}]}}\n" +
		"{\"timestamp\":\"2026-03-09T01:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"# AGENTS.md instructions for /tmp\\n\\n<INSTRUCTIONS>\\nhello\\n</INSTRUCTIONS>\"},{\"type\":\"input_text\",\"text\":\"<environment_context>\\nCurrent working directory: /tmp\\n</environment_context>\"}]}}\n" +
		"{\"timestamp\":\"2026-03-09T01:00:03Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\"}}\n" +
		"{\"timestamp\":\"2026-03-09T01:00:04Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"developer\",\"content\":[{\"type\":\"input_text\",\"text\":\"ignored again\"}]}}\n" +
		"{\"timestamp\":\"2026-03-09T01:00:05Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nFix it\"}]}}\n"

	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	session, err := ParseSession(filePath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if session.Meta == nil || session.Meta.Instructions != "base" {
		t.Fatalf("expected base instructions to populate session meta, got %#v", session.Meta)
	}
	if len(session.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(session.Items))
	}
	if !IsAutoContextUserMessage(session.Items[0].Content) {
		t.Fatalf("expected first item to be detected as auto context: %q", session.Items[0].Content)
	}
	if session.Items[1].Content != "Fix it" {
		t.Fatalf("expected trimmed user request, got %q", session.Items[1].Content)
	}
}

func TestParseSessionExtractsSubagentNotification(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "session.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-13T00:25:44Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"abc\",\"timestamp\":\"2026-03-13T00:25:44Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:25:44Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"spawn_agent\",\"arguments\":\"{\\\"message\\\":\\\"review BillingStatusService\\\"}\",\"call_id\":\"call_1\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:25:44Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_1\",\"output\":\"{\\\"agent_id\\\":\\\"agent-1\\\",\\\"nickname\\\":\\\"Anscombe\\\"}\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:25:45Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"<subagent_notification>\\n{\\\"agent_id\\\":\\\"agent-1\\\",\\\"status\\\":{\\\"completed\\\":\\\"done\\\"}}\\n</subagent_notification>\"}]}}\n" +
		"{\"timestamp\":\"2026-03-13T00:25:46Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nContinue\"}]}}\n"

	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	session, err := ParseSession(filePath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(session.Items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(session.Items))
	}
	call := session.Items[0]
	if call.Subtype != "function_call" || call.Role != "tool" {
		t.Fatalf("expected visible spawn_agent tool call, got %#v", call)
	}
	if !strings.Contains(call.Content, "review BillingStatusService") {
		t.Fatalf("expected spawn_agent request to stay visible, got %q", call.Content)
	}

	spawned := session.Items[1]
	if spawned.SubagentID != "agent-1" {
		t.Fatalf("expected spawned subagent id, got %q", spawned.SubagentID)
	}
	if spawned.SubagentStatusType != "spawned" {
		t.Fatalf("expected spawned status type, got %q", spawned.SubagentStatusType)
	}
	if !strings.Contains(spawned.Content, "\"nickname\": \"Anscombe\"") {
		t.Fatalf("expected spawned content to include nickname json, got %q", spawned.Content)
	}

	item := session.Items[2]
	if item.SubagentID != "agent-1" {
		t.Fatalf("expected subagent id, got %q", item.SubagentID)
	}
	if item.SubagentNickname != "Anscombe" {
		t.Fatalf("expected subagent nickname, got %q", item.SubagentNickname)
	}
	if item.SubagentStatusType != "completed" {
		t.Fatalf("expected subagent status type, got %q", item.SubagentStatusType)
	}
	if item.SubagentRequest != "review BillingStatusService" {
		t.Fatalf("expected subagent request, got %q", item.SubagentRequest)
	}
	if item.Title != "Subagent" {
		t.Fatalf("expected Subagent title, got %q", item.Title)
	}
	if item.Content != "done" {
		t.Fatalf("expected extracted notification content, got %q", item.Content)
	}
	if IsAutoContextUserMessage(item.Content) {
		t.Fatalf("expected subagent notification to stay visible")
	}
	if session.Items[3].Content != "Continue" {
		t.Fatalf("expected trimmed user request, got %q", session.Items[3].Content)
	}
}

func TestRenderFunctionCallOutputContentUsesLongerFenceWhenOutputContainsBackticks(t *testing.T) {
	output := "Chunk ID: a5b125\nOutput:\n\n```bash\n./gradlew clean build\n```\n"

	rendered := renderFunctionCallOutputContent("call_1", output)

	marker := "**Output**\n"
	index := strings.Index(rendered, marker)
	if index < 0 {
		t.Fatalf("expected output marker, got %q", rendered)
	}
	if !strings.HasPrefix(rendered[index+len(marker):], "````\n") {
		t.Fatalf("expected outer fence to expand beyond triple backticks, got %q", rendered[index+len(marker):])
	}
	if !strings.Contains(rendered, "\n````") {
		t.Fatalf("expected expanded closing fence, got %q", rendered)
	}
}

func TestRenderFunctionCallOutputContentExtractsTextPayload(t *testing.T) {
	output := `[
  {
    "text": "Current user is a member of 2 teams:\n\n<json>\n{\n  \"teams\": [\n    {\n      \"id\": \"team-1\"\n    }\n  ]\n}\n</json>",
    "type": "text"
  }
]`

	rendered := renderFunctionCallOutputContent("call_1", output)

	if !strings.Contains(rendered, "Current user is a member of 2 teams:") {
		t.Fatalf("expected extracted text payload, got %q", rendered)
	}
	if strings.Contains(rendered, "\"type\": \"text\"") || strings.Contains(rendered, "```") {
		t.Fatalf("expected raw json payload to stay hidden, got %q", rendered)
	}
	if !strings.Contains(rendered, "&lt;json&gt;") || !strings.Contains(rendered, "&lt;/json&gt;") {
		t.Fatalf("expected angle-bracket markers to stay visible as text, got %q", rendered)
	}
}

func TestRenderCustomToolCallContentFormatsApplyPatchAsDiff(t *testing.T) {
	input := "*** Begin Patch\n*** Update File: /tmp/file.txt\n@@\n-old\n+new\n*** End Patch"

	rendered := renderCustomToolCallContent("apply_patch", "completed", "call_patch", input)

	if !strings.Contains(rendered, "**Patch**") {
		t.Fatalf("expected patch label, got %q", rendered)
	}
	if !strings.Contains(rendered, "```diff") {
		t.Fatalf("expected diff fence, got %q", rendered)
	}
	if !strings.Contains(rendered, "*** Update File: /tmp/file.txt") || !strings.Contains(rendered, "+new") {
		t.Fatalf("expected patch body, got %q", rendered)
	}
}

func TestParseSessionKeepsMultipleSpawnedSubagentsSeparate(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "session.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-13T00:25:44Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"abc\",\"timestamp\":\"2026-03-13T00:25:44Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:25:44Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"spawn_agent\",\"arguments\":\"{\\\"message\\\":\\\"review A\\\"}\",\"call_id\":\"call_1\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:25:44Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"spawn_agent\",\"arguments\":\"{\\\"message\\\":\\\"review B\\\"}\",\"call_id\":\"call_2\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:25:44Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_1\",\"output\":\"{\\\"agent_id\\\":\\\"agent-1\\\",\\\"nickname\\\":\\\"Anscombe\\\"}\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:25:45Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_2\",\"output\":\"{\\\"agent_id\\\":\\\"agent-2\\\",\\\"nickname\\\":\\\"Boyle\\\"}\"}}\n"

	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	session, err := ParseSession(filePath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(session.Items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(session.Items))
	}
	if session.Items[0].Subtype != "function_call" || session.Items[1].Subtype != "function_call" {
		t.Fatalf("expected visible function calls to stay separate, got %#v", session.Items)
	}
	if session.Items[2].SubagentID != "agent-1" || session.Items[3].SubagentID != "agent-2" {
		t.Fatalf("expected spawned subagents to stay separate, got %#v", session.Items)
	}
}

func TestParseSessionShowsSelectedResponseItemsAndSkipsEncryptedOnlyReasoning(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "session.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-18T01:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"abc\",\"timestamp\":\"2026-03-18T01:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T01:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"web_search_call\",\"status\":\"completed\",\"action\":{\"type\":\"search\",\"query\":\"Codex CLI notify hook\",\"queries\":[\"Codex CLI notify hook\",\"OpenAI Codex notifications\"]}}}\n" +
		"{\"timestamp\":\"2026-03-18T01:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"custom_tool_call\",\"status\":\"completed\",\"call_id\":\"call_patch\",\"name\":\"apply_patch\",\"input\":\"*** Begin Patch\\n*** End Patch\"}}\n" +
		"{\"timestamp\":\"2026-03-18T01:00:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"custom_tool_call_output\",\"call_id\":\"call_patch\",\"output\":\"{\\\"output\\\":\\\"Success\\\",\\\"metadata\\\":{\\\"exit_code\\\":0}}\"}}\n" +
		"{\"timestamp\":\"2026-03-18T01:00:04Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"ghost_snapshot\",\"ghost_commit\":{\"id\":\"abc123\",\"parent\":\"def456\",\"preexisting_untracked_files\":[\"tmp/note.md\"],\"preexisting_untracked_dirs\":[]}}}\n" +
		"{\"timestamp\":\"2026-03-18T01:00:05Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"reasoning\",\"summary\":[],\"content\":null,\"encrypted_content\":\"secret\"}}\n" +
		"{\"timestamp\":\"2026-03-18T01:00:06Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"reasoning\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"Keep this\"}]}}\n"

	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	session, err := ParseSession(filePath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(session.Items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(session.Items))
	}
	if session.Items[0].Subtype != "web_search_call" || !strings.Contains(session.Items[0].Content, "Expanded queries") {
		t.Fatalf("expected visible web search call, got %#v", session.Items[0])
	}
	if session.Items[1].Subtype != "custom_tool_call" || !strings.Contains(session.Items[1].Content, "apply_patch") {
		t.Fatalf("expected visible custom tool call, got %#v", session.Items[1])
	}
	if session.Items[2].Subtype != "custom_tool_call_output" || !strings.Contains(session.Items[2].Content, "Success") {
		t.Fatalf("expected visible custom tool output, got %#v", session.Items[2])
	}
	if strings.Contains(session.Items[2].Content, "\"exit_code\": 0") || strings.Contains(session.Items[2].Content, "```") {
		t.Fatalf("expected custom tool output to use extracted plain output, got %#v", session.Items[2])
	}
	if session.Items[3].Subtype != "ghost_snapshot" || !strings.Contains(session.Items[3].Content, "abc123") {
		t.Fatalf("expected visible ghost snapshot, got %#v", session.Items[3])
	}
	if session.Items[4].Subtype != "reasoning" || session.Items[4].Content != "Keep this" {
		t.Fatalf("expected only non-empty reasoning to remain, got %#v", session.Items[4])
	}
}

func TestParseSessionSkipsQuerylessWebSearchCall(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "session.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-18T01:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"abc\",\"timestamp\":\"2026-03-18T01:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T01:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"web_search_call\",\"status\":\"completed\",\"action\":{\"type\":\"open_page\"}}}\n" +
		"{\"timestamp\":\"2026-03-18T01:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"visible\"}]}}\n"

	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	session, err := ParseSession(filePath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(session.Items) != 1 {
		t.Fatalf("expected only visible message item, got %d %#v", len(session.Items), session.Items)
	}
	if session.Items[0].Subtype != "message" || session.Items[0].Content != "visible" {
		t.Fatalf("expected assistant message to remain, got %#v", session.Items[0])
	}
}

func TestParseSessionFormatsExecCommandToolCallSummary(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "session.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-18T01:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"abc\",\"timestamp\":\"2026-03-18T01:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T01:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"sed -n '140,170p' /tmp/README.md\\\",\\\"workdir\\\":\\\"/tmp/project\\\",\\\"max_output_tokens\\\":4000}\",\"call_id\":\"call_exec\"}}\n"

	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	session, err := ParseSession(filePath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(session.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(session.Items))
	}
	item := session.Items[0]
	if item.Subtype != "function_call" {
		t.Fatalf("expected function_call item, got %#v", item)
	}
	if !strings.Contains(item.Content, "**Command**") || !strings.Contains(item.Content, "sed -n '140,170p' /tmp/README.md") {
		t.Fatalf("expected command summary, got %q", item.Content)
	}
	if !strings.Contains(item.Content, "**Workdir:** `/tmp/project`") {
		t.Fatalf("expected workdir summary, got %q", item.Content)
	}
	if strings.Contains(item.Content, "max_output_tokens") || strings.Contains(item.Content, "**Arguments**") {
		t.Fatalf("expected raw arguments to be hidden, got %q", item.Content)
	}
}

func TestParseSessionFormatsUpdatePlanToolCallSummary(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "session.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-18T01:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"abc\",\"timestamp\":\"2026-03-18T01:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T01:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"update_plan\",\"arguments\":\"{\\\"explanation\\\":\\\"Keep the session page in sync with /active.\\\",\\\"plan\\\":[{\\\"status\\\":\\\"completed\\\",\\\"step\\\":\\\"Add active state fields\\\"},{\\\"status\\\":\\\"in_progress\\\",\\\"step\\\":\\\"Render detailed page controls\\\"},{\\\"status\\\":\\\"pending\\\",\\\"step\\\":\\\"Run tests and update memo\\\"}]}\",\"call_id\":\"call_plan\"}}\n"

	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	session, err := ParseSession(filePath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(session.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(session.Items))
	}
	item := session.Items[0]
	if item.Subtype != "function_call" {
		t.Fatalf("expected function_call item, got %#v", item)
	}
	if !strings.Contains(item.Content, "**Plan**") || !strings.Contains(item.Content, "□ Render detailed page controls") {
		t.Fatalf("expected formatted plan summary, got %q", item.Content)
	}
	if !strings.Contains(item.Content, "✅ Add active state fields") || !strings.Contains(item.Content, "□ Run tests and update memo") {
		t.Fatalf("expected icon-based plan markers, got %q", item.Content)
	}
	if !strings.Contains(item.Content, "**Explanation**") || !strings.Contains(item.Content, "Keep the session page in sync with /active.") {
		t.Fatalf("expected explanation summary, got %q", item.Content)
	}
	if strings.Contains(item.Content, "\"status\"") || strings.Contains(item.Content, "**Arguments**") || strings.Contains(item.Content, "\"step\"") {
		t.Fatalf("expected raw arguments to stay hidden, got %q", item.Content)
	}
}

func TestReclassifyToolWarning(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "session.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-01-09T01:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"Warning: apply_patch was requested via exec_command. Use the apply_patch tool instead of exec_command.\"}]}}\n"

	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	session, err := ParseSession(filePath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(session.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(session.Items))
	}
	item := session.Items[0]
	if item.Role != "assistant" {
		t.Fatalf("expected assistant role, got %q", item.Role)
	}
	if item.Class != "role-assistant" {
		t.Fatalf("expected role-assistant class, got %q", item.Class)
	}
	if item.Title != "Agent" {
		t.Fatalf("expected Agent title, got %q", item.Title)
	}
}

func TestToolWarningWithExtraTextStaysUser(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "session.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-01-09T01:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"Warning: apply_patch was requested via exec_command. Use the apply_patch tool instead of exec_command.\\n\\nExtra note\"}]}}\n"

	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	session, err := ParseSession(filePath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(session.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(session.Items))
	}
	item := session.Items[0]
	if item.Role != "user" {
		t.Fatalf("expected user role, got %q", item.Role)
	}
	if item.Class != "role-user" {
		t.Fatalf("expected role-user class, got %q", item.Class)
	}
	if item.Title != "User" {
		t.Fatalf("expected User title, got %q", item.Title)
	}
}
