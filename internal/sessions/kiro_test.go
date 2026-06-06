package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseKiroSidecar(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "abc123.json")
	content := `{
  "session_id": "abc123-def456",
  "cwd": "/home/user/project",
  "created_at": "2026-05-27T04:18:08.480130713Z",
  "updated_at": "2026-05-27T04:54:12.541915932Z",
  "title": "Test session",
  "session_created_reason": "subagent"
}`
	if err := os.WriteFile(jsonPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	meta, err := ParseKiroSidecar(jsonPath)
	if err != nil {
		t.Fatalf("ParseKiroSidecar: %v", err)
	}
	if meta.ID != "abc123-def456" {
		t.Errorf("ID = %q, want abc123-def456", meta.ID)
	}
	if meta.Cwd != "/home/user/project" {
		t.Errorf("Cwd = %q, want /home/user/project", meta.Cwd)
	}
	if meta.Originator != "kiro" {
		t.Errorf("Originator = %q, want kiro", meta.Originator)
	}
	if meta.Timestamp != "2026-05-27T04:18:08.480130713Z" {
		t.Errorf("Timestamp = %q", meta.Timestamp)
	}
}

func TestKiroSidecarDate(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "test.json")
	content := `{"session_id":"x","cwd":"/tmp","created_at":"2026-05-27T04:18:08.480130713Z","updated_at":"2026-05-27T04:54:12Z","title":"t"}`
	if err := os.WriteFile(jsonPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	date, ok := KiroSidecarDate(jsonPath)
	if !ok {
		t.Fatal("KiroSidecarDate returned false")
	}
	if date.Year != "2026" || date.Month != "05" || date.Day != "27" {
		t.Errorf("date = %v, want 2026/05/27", date)
	}
}

func TestParseKiroSession(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "session.jsonl")
	lines := `{"version":"v1","kind":"Prompt","data":{"message_id":"msg1","content":[{"kind":"text","data":"Hello, help me"}],"meta":{"timestamp":1779855560}}}
{"version":"v1","kind":"AssistantMessage","data":{"message_id":"msg2","content":[{"kind":"text","data":"Sure, I can help."},{"kind":"toolUse","data":{"toolUseId":"tool1","name":"shell","input":{"command":"ls"}}}],"meta":{"timestamp":1779855570}}}
{"version":"v1","kind":"ToolResults","data":{"message_id":"msg3","content":[{"kind":"toolResult","data":{"toolUseId":"tool1","content":[{"kind":"text","data":"file1.txt\nfile2.txt"}]}}],"meta":{"timestamp":1779855575}}}
`
	if err := os.WriteFile(jsonlPath, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := ParseKiroSession(jsonlPath)
	if err != nil {
		t.Fatalf("ParseKiroSession: %v", err)
	}

	if len(session.Items) != 4 {
		t.Fatalf("got %d items, want 4", len(session.Items))
	}

	// Item 0: user prompt
	if session.Items[0].Role != "user" {
		t.Errorf("item 0 role = %q, want user", session.Items[0].Role)
	}
	if session.Items[0].Content != "Hello, help me" {
		t.Errorf("item 0 content = %q", session.Items[0].Content)
	}

	// Item 1: assistant text
	if session.Items[1].Role != "assistant" {
		t.Errorf("item 1 role = %q, want assistant", session.Items[1].Role)
	}
	if session.Items[1].Content != "Sure, I can help." {
		t.Errorf("item 1 content = %q", session.Items[1].Content)
	}

	// Item 2: tool call
	if session.Items[2].Subtype != "function_call" {
		t.Errorf("item 2 subtype = %q, want function_call", session.Items[2].Subtype)
	}
	if session.Items[2].ToolName != "shell" {
		t.Errorf("item 2 tool name = %q, want shell", session.Items[2].ToolName)
	}

	// Item 3: tool result
	if session.Items[3].Subtype != "function_call_output" {
		t.Errorf("item 3 subtype = %q, want function_call_output", session.Items[3].Subtype)
	}
	if !strings.Contains(session.Items[3].Content, "**Call ID:** tool1") {
		t.Errorf("item 3 content missing call id, got %q", session.Items[3].Content)
	}
	if !strings.Contains(session.Items[3].Content, "file1.txt") {
		t.Errorf("item 3 content missing output text, got %q", session.Items[3].Content)
	}
}
