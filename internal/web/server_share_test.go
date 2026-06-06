package web

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codex-manager/internal/render"
	"codex-manager/internal/sessions"
	"net/http"
	"net/http/httptest"
)

type fakeHTMLBucketUploader struct {
	url   string
	err   error
	calls int
	html  string
}

func (f *fakeHTMLBucketUploader) Upload(_ context.Context, html string) (string, error) {
	f.calls++
	f.html = html
	if f.err != nil {
		return "", f.err
	}
	return f.url, nil
}

func TestHandleShareLocalWritesFile(t *testing.T) {
	sessionsDir := t.TempDir()
	shareDir := filepath.Join(t.TempDir(), "shares")
	datePath, fileName := writeTestSession(t, sessionsDir)

	idx := sessions.NewIndex(sessionsDir, "")
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	renderer, err := render.New()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	server := NewServer(idx, nil, renderer, sessionsDir, shareDir, ":8081", 3)
	req := httptest.NewRequest(http.MethodPost, "http://example.com/share/"+datePath+"/"+fileName, nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", rec.Code, rec.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	url := payload["url"]
	if !strings.HasPrefix(url, "http://example.com:8081/") {
		t.Fatalf("unexpected url: %q", url)
	}

	entries, err := os.ReadDir(shareDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 share file, got %d", len(entries))
	}
}

func TestHandleShareHTMLBucketSuccess(t *testing.T) {
	sessionsDir := t.TempDir()
	shareDir := filepath.Join(t.TempDir(), "shares")
	datePath, fileName := writeTestSession(t, sessionsDir)

	idx := sessions.NewIndex(sessionsDir, "")
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	renderer, err := render.New()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	uploader := &fakeHTMLBucketUploader{url: "https://abc123.htmlbucket.com"}
	server := NewServer(idx, nil, renderer, sessionsDir, shareDir, ":8081", 3)
	server.EnableHTMLBucket(uploader)

	req := httptest.NewRequest(http.MethodPost, "http://example.com/share/"+datePath+"/"+fileName, nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", rec.Code, rec.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["url"] != uploader.url {
		t.Fatalf("unexpected url: %q", payload["url"])
	}
	if uploader.calls != 1 {
		t.Fatalf("expected one upload call, got %d", uploader.calls)
	}
	if !strings.Contains(strings.ToLower(uploader.html), "<!doctype html>") {
		t.Fatalf("expected rendered html payload")
	}

	if _, err := os.Stat(shareDir); err == nil {
		entries, readErr := os.ReadDir(shareDir)
		if readErr != nil {
			t.Fatalf("readdir: %v", readErr)
		}
		if len(entries) != 0 {
			t.Fatalf("expected no local share files, got %d", len(entries))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat share dir: %v", err)
	}
}

func TestHandleShareHTMLBucketFailure(t *testing.T) {
	sessionsDir := t.TempDir()
	shareDir := filepath.Join(t.TempDir(), "shares")
	datePath, fileName := writeTestSession(t, sessionsDir)

	idx := sessions.NewIndex(sessionsDir, "")
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	renderer, err := render.New()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	uploader := &fakeHTMLBucketUploader{err: errors.New("boom")}
	server := NewServer(idx, nil, renderer, sessionsDir, shareDir, ":8081", 3)
	server.EnableHTMLBucket(uploader)

	req := httptest.NewRequest(http.MethodPost, "http://example.com/share/"+datePath+"/"+fileName, nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d body %s", rec.Code, rec.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(payload["error"], "htmlbucket upload failed") {
		t.Fatalf("unexpected error payload: %q", payload["error"])
	}
}

func writeTestSession(t *testing.T, sessionsDir string) (string, string) {
	t.Helper()
	datePath := filepath.Join("2026", "01", "09")
	fileName := "session.jsonl"
	fullDir := filepath.Join(sessionsDir, datePath)
	if err := os.MkdirAll(fullDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data := "" +
		"{\"timestamp\":\"2026-01-09T01:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"abc\",\"timestamp\":\"2026-01-09T01:00:00Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-01-09T01:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nHello\"}]}}\n" +
		"{\"timestamp\":\"2026-01-09T01:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Hi\"}]}}\n"
	fullPath := filepath.Join(fullDir, fileName)
	if err := os.WriteFile(fullPath, []byte(data), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return filepath.ToSlash(datePath), fileName
}
