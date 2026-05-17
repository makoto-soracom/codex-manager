package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codex-manager/internal/render"
	"codex-manager/internal/sessions"
)

func TestHandleSessionShortensAssistantPathsRelativeToCwd(t *testing.T) {
	sessionsDir := t.TempDir()
	datePath := filepath.Join("2026", "04", "30")
	dateDir := filepath.Join(sessionsDir, datePath)
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fileName := "session.jsonl"
	data := "" +
		"{\"timestamp\":\"2026-04-30T01:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"abc\",\"timestamp\":\"2026-04-30T01:00:00Z\",\"cwd\":\"/tmp/app\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-04-30T01:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nShow paths\"}]}}\n" +
		"{\"timestamp\":\"2026-04-30T01:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Changed /tmp/app/src/main.go:12 and left /tmp/app-other/src/main.go alone\"}]}}\n"
	if err := os.WriteFile(filepath.Join(dateDir, fileName), []byte(data), 0o600); err != nil {
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
	server := NewServer(idx, nil, renderer, sessionsDir, filepath.Join(t.TempDir(), "shares"), ":8081", 3)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/2026/04/30/"+fileName, nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Changed src/main.go:12") {
		t.Fatalf("expected assistant path to be relative, body=%s", body)
	}
	if strings.Contains(body, "/tmp/app/src/main.go:12") {
		t.Fatalf("expected assistant full path to be hidden, body=%s", body)
	}
	if !strings.Contains(body, "/tmp/app-other/src/main.go") {
		t.Fatalf("expected sibling absolute path to stay visible, body=%s", body)
	}
	if !strings.Contains(body, "CWD: /tmp/app") {
		t.Fatalf("expected page cwd label to stay absolute, body=%s", body)
	}
}
