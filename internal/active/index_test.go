package active

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codex-manager/internal/sessions"
)

func TestIndexRefreshFromBuildsTopLevelSummary(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	dateDir := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "session_index.jsonl"), []byte("{\"id\":\"parent-1\",\"thread_name\":\"parser bug\",\"updated_at\":\"2026-03-18T08:00:00Z\"}\n"), 0o600); err != nil {
		t.Fatalf("write session index: %v", err)
	}

	parentPath := filepath.Join(dateDir, "parent.jsonl")
	parentData := "" +
		"{\"timestamp\":\"2026-03-18T07:59:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"parent-1\",\"timestamp\":\"2026-03-18T07:59:00Z\",\"cwd\":\"/tmp/project\",\"git\":{\"branch\":\"feature/parser-fix\",\"commit_hash\":\"abc123\"},\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T07:59:10Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nFix the parser\"}]}}\n" +
		"{\"timestamp\":\"2026-03-18T07:59:12Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Investigating now.\"}]}}\n" +
		"{\"timestamp\":\"2026-03-18T08:00:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\"}}\n"
	if err := os.WriteFile(parentPath, []byte(parentData), 0o600); err != nil {
		t.Fatalf("write parent session: %v", err)
	}

	subagentPath := filepath.Join(dateDir, "subagent.jsonl")
	subagentData := "" +
		"{\"timestamp\":\"2026-03-18T08:01:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"agent-1\",\"forked_from_id\":\"parent-1\",\"timestamp\":\"2026-03-18T08:01:00Z\",\"cwd\":\"/tmp/project\",\"originator\":\"cli\",\"cli_version\":\"0.1\",\"source\":{\"subagent\":{\"thread_spawn\":{\"parent_thread_id\":\"parent-1\",\"depth\":1,\"agent_nickname\":\"Anscombe\",\"agent_role\":\"explorer\"}}}}}\n" +
		"{\"timestamp\":\"2026-03-18T08:01:10Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Subagent work.\"}]}}\n"
	if err := os.WriteFile(subagentPath, []byte(subagentData), 0o600); err != nil {
		t.Fatalf("write subagent session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir, "")
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh sessions: %v", err)
	}

	activeIdx := NewIndex()
	if err := activeIdx.RefreshFrom(idx); err != nil {
		t.Fatalf("refresh active index: %v", err)
	}

	summaries := activeIdx.Summaries()
	if len(summaries) != 1 {
		t.Fatalf("expected 1 top-level summary, got %d", len(summaries))
	}

	summary := summaries[0]
	if summary.Key != "id:parent-1" {
		t.Fatalf("unexpected key: %q", summary.Key)
	}
	if summary.DisplayName != "parser bug (parent.jsonl)" {
		t.Fatalf("unexpected display name: %q", summary.DisplayName)
	}
	if summary.WaitState != WaitStateAgent {
		t.Fatalf("expected agent wait state, got %q", summary.WaitState)
	}
	if summary.LastActivityAt.Format("2006-01-02T15:04:05Z07:00") != "2026-03-18T08:00:00Z" {
		t.Fatalf("unexpected last activity: %s", summary.LastActivityAt.Format(timeLayout))
	}
	if summary.LastUserSnippet.Text != "Fix the parser" {
		t.Fatalf("unexpected user snippet: %q", summary.LastUserSnippet.Text)
	}
	if summary.LastAssistantSnippet.Text != "Investigating now." {
		t.Fatalf("unexpected assistant snippet: %q", summary.LastAssistantSnippet.Text)
	}
	if summary.Branch != "feature/parser-fix" {
		t.Fatalf("unexpected branch: %q", summary.Branch)
	}
	if summary.ResumeCommand != "cd '/tmp/project'\ngit switch 'feature/parser-fix'\ncodex resume parent-1" {
		t.Fatalf("unexpected resume command: %q", summary.ResumeCommand)
	}
}

func TestIndexRefreshFromUsesThinkingPlaceholderWhenLatestUserHasNoAssistantReply(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	dateDir := filepath.Join(sessionsDir, "2026", "03", "19")
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	filePath := filepath.Join(dateDir, "thinking.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-19T01:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-thinking\",\"timestamp\":\"2026-03-19T01:00:00Z\",\"cwd\":\"/tmp/project\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-19T01:00:05Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nEarlier request\"}]}}\n" +
		"{\"timestamp\":\"2026-03-19T01:00:10Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Older assistant message.\"}]}}\n" +
		"{\"timestamp\":\"2026-03-19T01:00:20Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nNewest request\"}]}}\n" +
		"{\"timestamp\":\"2026-03-19T01:00:30Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\"}}\n"
	if err := os.WriteFile(filePath, []byte(data), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir, "")
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh sessions: %v", err)
	}

	activeIdx := NewIndex()
	if err := activeIdx.RefreshFrom(idx); err != nil {
		t.Fatalf("refresh active index: %v", err)
	}

	summaries := activeIdx.Summaries()
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}

	summary := summaries[0]
	if summary.WaitState != WaitStateAgent {
		t.Fatalf("expected agent wait state, got %q", summary.WaitState)
	}
	if summary.LastUserSnippet.Text != "Newest request" {
		t.Fatalf("unexpected user snippet: %q", summary.LastUserSnippet.Text)
	}
	if summary.LastAssistantSnippet.Text != thinkingPlaceholder {
		t.Fatalf("expected thinking placeholder, got %q", summary.LastAssistantSnippet.Text)
	}
}

func TestIndexRefreshFromHandlesLongJSONLLines(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	dateDir := filepath.Join(sessionsDir, "2026", "03", "20")
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	filePath := filepath.Join(dateDir, "long-line.jsonl")
	longOutput := strings.Repeat("x", 4*1024*1024+1)
	data := "" +
		"{\"timestamp\":\"2026-03-20T01:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-long-line\",\"timestamp\":\"2026-03-20T01:00:00Z\",\"cwd\":\"/tmp/project\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-20T01:00:05Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nHandle long lines\"}]}}\n" +
		"{\"timestamp\":\"2026-03-20T01:00:10Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call_long\",\"output\":\"" + longOutput + "\"}}\n" +
		"{\"timestamp\":\"2026-03-20T01:00:20Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\"}}\n"
	if err := os.WriteFile(filePath, []byte(data), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir, "")
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh sessions: %v", err)
	}

	activeIdx := NewIndex()
	if err := activeIdx.RefreshFrom(idx); err != nil {
		t.Fatalf("refresh active index: %v", err)
	}

	summaries := activeIdx.Summaries()
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].WaitState != WaitStateAgent {
		t.Fatalf("expected agent wait state, got %q", summaries[0].WaitState)
	}
	if summaries[0].LastActivityAt.Format(timeLayout) != "2026-03-20T01:00:20Z" {
		t.Fatalf("unexpected last activity: %s", summaries[0].LastActivityAt.Format(timeLayout))
	}
}

func TestStateStoreReconcileClearsEndedMarkOnNewActivity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session_state.json")

	store, err := LoadStateStore(path)
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	if err := store.MarkEnded("id:session-1", "token-1"); err != nil {
		t.Fatalf("mark ended: %v", err)
	}
	if got := len(store.Snapshot()); got != 1 {
		t.Fatalf("expected 1 ended mark, got %d", got)
	}

	if err := store.Reconcile([]Summary{{Key: "id:session-1", ActivityToken: "token-2"}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := len(store.Snapshot()); got != 0 {
		t.Fatalf("expected ended mark to clear, got %d", got)
	}

	reloaded, err := LoadStateStore(path)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if got := len(reloaded.Snapshot()); got != 0 {
		t.Fatalf("expected persisted ended mark to clear, got %d", got)
	}
}

const timeLayout = "2006-01-02T15:04:05Z07:00"
