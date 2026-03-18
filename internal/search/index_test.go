package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"codex-manager/internal/sessions"
)

func TestIndexSearch(t *testing.T) {
	baseDir := t.TempDir()
	writeSessionFile(t, baseDir, "2024/01/02/session.jsonl", []string{
		`{"timestamp":"t1","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"text","text":"Hello world"}]}}`,
		`{"timestamp":"t2","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"text","text":"Here is a search result"}]}}`,
	})

	idx := sessions.NewIndex(baseDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	searchIdx := NewIndex()
	if err := searchIdx.RefreshFrom(idx); err != nil {
		t.Fatalf("search refresh: %v", err)
	}

	results := searchIdx.Search("hello", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].File != "session.jsonl" {
		t.Fatalf("expected file session.jsonl, got %q", results[0].File)
	}
	if results[0].Line != 1 {
		t.Fatalf("expected line 1, got %d", results[0].Line)
	}
	if !strings.Contains(results[0].Preview, "Hello") {
		t.Fatalf("expected preview to contain Hello, got %q", results[0].Preview)
	}

	writeSessionFile(t, baseDir, "2024/01/02/session.jsonl", []string{
		`{"timestamp":"t3","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"text","text":"Updated content for search"}]}}`,
	})
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh after update: %v", err)
	}
	if err := searchIdx.RefreshFrom(idx); err != nil {
		t.Fatalf("search refresh after update: %v", err)
	}

	results = searchIdx.Search("hello", 10)
	if len(results) != 0 {
		t.Fatalf("expected 0 results after update, got %d", len(results))
	}
	results = searchIdx.Search("updated", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for updated, got %d", len(results))
	}
	if results[0].Line != 1 {
		t.Fatalf("expected line 1 after update, got %d", results[0].Line)
	}
}

func TestSearchDeduplicatesConsecutiveUserAssistantHits(t *testing.T) {
	baseDir := t.TempDir()
	writeSessionFile(t, baseDir, "2024/01/02/consecutive.jsonl", []string{
		`{"timestamp":"2024-01-02T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"text","text":"キーワード を含む質問"}]}}`,
		`{"timestamp":"2024-01-02T00:00:02Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"text","text":"キーワード を含む回答"}]}}`,
	})

	idx := sessions.NewIndex(baseDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	searchIdx := NewIndex()
	if err := searchIdx.RefreshFrom(idx); err != nil {
		t.Fatalf("search refresh: %v", err)
	}

	results := searchIdx.Search("キーワード", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 deduplicated result, got %d", len(results))
	}
	if results[0].Role != "user" {
		t.Fatalf("expected user result to be kept, got %q", results[0].Role)
	}
	if results[0].Line != 1 {
		t.Fatalf("expected user line 1, got %d", results[0].Line)
	}
	if results[0].NextAssistantLine != 2 {
		t.Fatalf("expected next assistant line 2, got %d", results[0].NextAssistantLine)
	}
}

func TestSearchKeepsAssistantOnlyHit(t *testing.T) {
	baseDir := t.TempDir()
	writeSessionFile(t, baseDir, "2024/01/02/assistant-only.jsonl", []string{
		`{"timestamp":"2024-01-02T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"text","text":"質問だけ"}]}}`,
		`{"timestamp":"2024-01-02T00:00:02Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"text","text":"キーワード を含む回答"}]}}`,
	})

	idx := sessions.NewIndex(baseDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	searchIdx := NewIndex()
	if err := searchIdx.RefreshFrom(idx); err != nil {
		t.Fatalf("search refresh: %v", err)
	}

	results := searchIdx.Search("キーワード", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 assistant-only result, got %d", len(results))
	}
	if results[0].Role != "assistant" {
		t.Fatalf("expected assistant result, got %q", results[0].Role)
	}
	if results[0].Line != 2 {
		t.Fatalf("expected assistant line 2, got %d", results[0].Line)
	}
}

func TestSearchIncludesDisplayFile(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	indexPath := filepath.Join(root, "session_index.jsonl")
	if err := os.WriteFile(indexPath, []byte("{\"id\":\"session-1\",\"thread_name\":\"pr461 #3\",\"updated_at\":\"2026-03-13T06:09:42Z\"}\n"), 0o600); err != nil {
		t.Fatalf("write session index: %v", err)
	}

	writeSessionFile(t, sessionsDir, "2026/03/13/rollout-2026-03-13T13-36-02-session-1.jsonl", []string{
		`{"timestamp":"2026-03-13T00:00:00Z","type":"session_meta","payload":{"id":"session-1","timestamp":"2026-03-13T00:00:00Z","cwd":"/tmp","originator":"cli","cli_version":"0.1","source":"cli"}}`,
		`{"timestamp":"2026-03-13T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"text","text":"Please check the display name"}]}}`,
	})

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	searchIdx := NewIndex()
	if err := searchIdx.RefreshFrom(idx); err != nil {
		t.Fatalf("search refresh: %v", err)
	}

	results := searchIdx.Search("display name", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	want := "pr461 #3 (rollout-2026-03-13T13-36-02-session-1.jsonl)"
	if results[0].DisplayFile != want {
		t.Fatalf("expected display file %q, got %q", want, results[0].DisplayFile)
	}

	if err := os.WriteFile(indexPath, []byte("{\"id\":\"session-1\",\"thread_name\":\"pr461 #4\",\"updated_at\":\"2026-03-13T07:09:42Z\"}\n"), 0o600); err != nil {
		t.Fatalf("rewrite session index: %v", err)
	}
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh after session index update: %v", err)
	}
	if err := searchIdx.RefreshFrom(idx); err != nil {
		t.Fatalf("search refresh after session index update: %v", err)
	}

	results = searchIdx.Search("display name", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result after update, got %d", len(results))
	}
	want = "pr461 #4 (rollout-2026-03-13T13-36-02-session-1.jsonl)"
	if results[0].DisplayFile != want {
		t.Fatalf("expected updated display file %q, got %q", want, results[0].DisplayFile)
	}
}

func TestMakePreviewMultibyteBoundarySafe(t *testing.T) {
	content := strings.Repeat("あ", 120) + "キーワード" + strings.Repeat("い", 120)
	preview := makePreview(content, "キーワード")

	if preview == "" {
		t.Fatalf("expected preview, got empty")
	}
	if !utf8.ValidString(preview) {
		t.Fatalf("expected valid UTF-8 preview, got %q", preview)
	}
	if strings.ContainsRune(preview, '\uFFFD') {
		t.Fatalf("expected preview without replacement rune, got %q", preview)
	}
	if !strings.Contains(preview, "キーワード") {
		t.Fatalf("expected preview to contain query, got %q", preview)
	}
}

func writeSessionFile(t *testing.T, baseDir, relPath string, lines []string) {
	t.Helper()
	fullPath := filepath.Join(baseDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	payload := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(fullPath, []byte(payload), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
