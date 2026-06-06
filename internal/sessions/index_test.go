package sessions

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIndexRefreshAndLookup(t *testing.T) {
	base := t.TempDir()
	pathA := filepath.Join(base, "2026", "01", "09")
	if err := os.MkdirAll(pathA, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	filePath := filepath.Join(pathA, "session-a.jsonl")
	if err := os.WriteFile(filePath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chtimes(filePath, time.Now(), time.Now()); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	idx := NewIndex(base, "")
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	dates := idx.Dates()
	if len(dates) != 1 {
		t.Fatalf("expected 1 date, got %d", len(dates))
	}
	date := dates[0]
	if date.String() != "2026-01-09" {
		t.Fatalf("unexpected date: %s", date.String())
	}

	files := idx.SessionsByDate(date)
	if len(files) != 1 {
		t.Fatalf("expected 1 session, got %d", len(files))
	}
	if files[0].Name != "session-a.jsonl" {
		t.Fatalf("unexpected file: %s", files[0].Name)
	}

	lookup, ok := idx.Lookup(date, "session-a.jsonl")
	if !ok {
		t.Fatalf("expected lookup to succeed")
	}
	if lookup.Path != filePath {
		t.Fatalf("unexpected path: %s", lookup.Path)
	}
}

func TestIndexLookupByID(t *testing.T) {
	base := t.TempDir()
	pathA := filepath.Join(base, "2026", "03", "13")
	if err := os.MkdirAll(pathA, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	filePath := filepath.Join(pathA, "session-a.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-13T00:25:44Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"agent-1\",\"timestamp\":\"2026-03-13T00:25:44Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\",\"source\":\"cli\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:25:45Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}}\n"
	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	idx := NewIndex(base, "")
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	lookup, ok := idx.LookupByID("agent-1")
	if !ok {
		t.Fatalf("expected lookup by id to succeed")
	}
	if lookup.Path != filePath {
		t.Fatalf("unexpected path: %s", lookup.Path)
	}
}

func TestIndexRefreshCapturesGitBranchFromSessionMeta(t *testing.T) {
	base := t.TempDir()
	pathA := filepath.Join(base, "2026", "03", "19")
	if err := os.MkdirAll(pathA, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	filePath := filepath.Join(pathA, "session-a.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-19T00:25:44Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"agent-1\",\"timestamp\":\"2026-03-19T00:25:44Z\",\"cwd\":\"/tmp\",\"git\":{\"branch\":\"feature/session-branch\",\"commit_hash\":\"abc123\"},\"originator\":\"cli\",\"cli_version\":\"0.1\",\"source\":\"cli\"}}\n" +
		"{\"timestamp\":\"2026-03-19T00:25:45Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}}\n"
	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	idx := NewIndex(base, "")
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	date, ok := ParseDate("2026", "03", "19")
	if !ok {
		t.Fatal("expected valid date")
	}
	lookup, ok := idx.Lookup(date, "session-a.jsonl")
	if !ok {
		t.Fatalf("expected lookup to succeed")
	}
	if lookup.Meta == nil {
		t.Fatal("expected meta")
	}
	if lookup.Meta.GitBranch() != "feature/session-branch" {
		t.Fatalf("expected git branch, got %q", lookup.Meta.GitBranch())
	}
}

func TestIndexRefreshUsesLatestThreadName(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	datePath := filepath.Join(sessionsDir, "2026", "03", "13")
	if err := os.MkdirAll(datePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	indexPath := filepath.Join(root, "session_index.jsonl")
	indexData := "" +
		"{\"id\":\"session-1\",\"thread_name\":\"old name\",\"updated_at\":\"2026-03-12T00:00:00Z\"}\n" +
		"{\"id\":\"session-1\",\"thread_name\":\"new name\",\"updated_at\":\"2026-03-13T00:00:00Z\"}\n"
	if err := os.WriteFile(indexPath, []byte(indexData), 0o644); err != nil {
		t.Fatalf("write session index: %v", err)
	}

	filePath := filepath.Join(datePath, "rollout-2026-03-13T13-36-02-session-1.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-13T00:25:44Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-1\",\"timestamp\":\"2026-03-13T00:25:44Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\",\"source\":\"cli\"}}\n" +
		"{\"timestamp\":\"2026-03-13T00:25:45Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}}\n"
	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	idx := NewIndex(sessionsDir, "")
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	date, ok := ParseDate("2026", "03", "13")
	if !ok {
		t.Fatal("expected valid date")
	}
	file, ok := idx.Lookup(date, "rollout-2026-03-13T13-36-02-session-1.jsonl")
	if !ok {
		t.Fatal("expected lookup to succeed")
	}
	if file.ThreadName != "new name" {
		t.Fatalf("expected latest thread name, got %q", file.ThreadName)
	}
	if file.DisplayName() != "new name (rollout-2026-03-13T13-36-02-session-1.jsonl)" {
		t.Fatalf("unexpected display name: %q", file.DisplayName())
	}
	if got := idx.ThreadName("session-1"); got != "new name" {
		t.Fatalf("expected thread name lookup, got %q", got)
	}
}
