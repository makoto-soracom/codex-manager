package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codex-manager/internal/notifications"
	"codex-manager/internal/render"
	"codex-manager/internal/sessions"
)

func TestHandleHookStoresNotificationAndRendersNotificationsPage(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh sessions: %v", err)
	}

	renderer, err := render.New()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	store, err := notifications.LoadStore(filepath.Join(root, "notifications.jsonl"))
	if err != nil {
		t.Fatalf("load notifications: %v", err)
	}

	server := NewServer(idx, nil, renderer, sessionsDir, "", "", 3)
	server.EnableNotifications(store)

	payload := `{"type":"task_complete","message":"done"}`
	hookReq := httptest.NewRequest(http.MethodPost, "http://example.com/hook", strings.NewReader(payload))
	hookReq.Header.Set("Content-Type", "application/json")
	hookReq.Header.Set("X-Codex-Test", "yes")
	hookRec := httptest.NewRecorder()
	server.ServeHTTP(hookRec, hookReq)

	if hookRec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d body %s", hookRec.Code, hookRec.Body.String())
	}
	if got := len(store.Entries()); got != 1 {
		t.Fatalf("expected 1 notification, got %d", got)
	}

	pageReq := httptest.NewRequest(http.MethodGet, "http://example.com/notifications", nil)
	pageRec := httptest.NewRecorder()
	server.ServeHTTP(pageRec, pageReq)

	if pageRec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", pageRec.Code, pageRec.Body.String())
	}
	body := pageRec.Body.String()
	if !strings.Contains(body, "POST /hook") {
		t.Fatalf("expected hook entry, body=%s", body)
	}
	if !strings.Contains(body, "task_complete") {
		t.Fatalf("expected JSON payload, body=%s", body)
	}
	if !strings.Contains(body, "X-Codex-Test") {
		t.Fatalf("expected headers to render, body=%s", body)
	}
}
