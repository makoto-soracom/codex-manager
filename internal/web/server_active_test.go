package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codex-manager/internal/active"
	"codex-manager/internal/render"
	"codex-manager/internal/sessions"
)

func TestHandleActiveUsesCookieTimeZoneForDayFilter(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(filepath.Join(sessionsDir, "2026", "03", "18"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "session_index.jsonl"), []byte("{\"id\":\"session-1\",\"thread_name\":\"tz thread\",\"updated_at\":\"2026-03-18T00:30:00Z\"}\n"), 0o600); err != nil {
		t.Fatalf("write session index: %v", err)
	}

	filePath := filepath.Join(sessionsDir, "2026", "03", "18", "cross-day.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-18T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-1\",\"timestamp\":\"2026-03-18T00:00:00Z\",\"cwd\":\"/tmp/app\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T00:10:00Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nContinue work\"}]}}\n" +
		"{\"timestamp\":\"2026-03-18T00:20:00Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Still working\"}]}}\n" +
		"{\"timestamp\":\"2026-03-18T00:30:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n"
	if err := os.WriteFile(filePath, []byte(data), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	server := newActiveTestServer(t, sessionsDir)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/active?date=2026-03-17", nil)
	req.AddCookie(&http.Cookie{Name: activeTimeZoneCookie, Value: "America%2FLos_Angeles"})
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "tz thread (cross-day.jsonl)") {
		t.Fatalf("expected thread to appear for Los Angeles day view, body=%s", body)
	} else if !strings.Contains(body, "▶️ Copy resume") {
		t.Fatalf("expected resume button icon, body=%s", body)
	} else if !strings.Contains(body, "⏹️ End") {
		t.Fatalf("expected end button icon, body=%s", body)
	} else if !strings.Contains(body, "thread-status-waiting-user") {
		t.Fatalf("expected waiting-user card class on active page, body=%s", body)
	}

	reqJST := httptest.NewRequest(http.MethodGet, "http://example.com/active?date=2026-03-17", nil)
	reqJST.AddCookie(&http.Cookie{Name: activeTimeZoneCookie, Value: "Asia%2FTokyo"})
	recJST := httptest.NewRecorder()
	server.ServeHTTP(recJST, reqJST)

	if recJST.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", recJST.Code, recJST.Body.String())
	}
	if body := recJST.Body.String(); strings.Contains(body, "tz thread (cross-day.jsonl)") {
		t.Fatalf("expected thread to be absent for Tokyo day view, body=%s", body)
	}
}

func TestHandleActiveShowsThinkingPlaceholderForUnansweredUserMessage(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	dateDir := filepath.Join(sessionsDir, "2026", "03", "19")
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	filePath := filepath.Join(dateDir, "thinking.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-19T02:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-thinking\",\"timestamp\":\"2026-03-19T02:00:00Z\",\"cwd\":\"/tmp/app\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-19T02:00:05Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nOld request\"}]}}\n" +
		"{\"timestamp\":\"2026-03-19T02:00:10Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Old assistant reply\"}]}}\n" +
		"{\"timestamp\":\"2026-03-19T02:00:20Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nNewest request\"}]}}\n" +
		"{\"timestamp\":\"2026-03-19T02:00:30Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\"}}\n"
	if err := os.WriteFile(filePath, []byte(data), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	server := newActiveTestServer(t, sessionsDir)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/active?scope=all", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Newest request") {
		t.Fatalf("expected latest user snippet, body=%s", body)
	}
	if !strings.Contains(body, "Thinking...") {
		t.Fatalf("expected thinking placeholder, body=%s", body)
	}
	if strings.Contains(body, "Old assistant reply") {
		t.Fatalf("expected stale assistant snippet to be hidden, body=%s", body)
	}
}

func TestHandleActiveShowsBranchAndBranchAwareResumeCommand(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	dateDir := filepath.Join(sessionsDir, "2026", "03", "19")
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	filePath := filepath.Join(dateDir, "branch.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-19T02:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-branch\",\"timestamp\":\"2026-03-19T02:00:00Z\",\"cwd\":\"/tmp/app\",\"git\":{\"branch\":\"feature/active-branch\",\"commit_hash\":\"abc123\"},\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-19T02:00:05Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nResume me\"}]}}\n" +
		"{\"timestamp\":\"2026-03-19T02:00:10Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n"
	if err := os.WriteFile(filePath, []byte(data), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	server := newActiveTestServer(t, sessionsDir)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/active?scope=all", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Branch: feature/active-branch") {
		t.Fatalf("expected branch label on active page, body=%s", body)
	}
	if !strings.Contains(body, "git switch &#39;feature/active-branch&#39;") {
		t.Fatalf("expected branch-aware resume command on active page, body=%s", body)
	}
}

func TestHandleActiveShowsDateDividersForAllActiveThreads(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	date19Dir := filepath.Join(sessionsDir, "2026", "03", "19")
	date18Dir := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(date19Dir, 0o755); err != nil {
		t.Fatalf("mkdir 19: %v", err)
	}
	if err := os.MkdirAll(date18Dir, 0o755); err != nil {
		t.Fatalf("mkdir 18: %v", err)
	}

	newerPath := filepath.Join(date19Dir, "newer.jsonl")
	newerData := "" +
		"{\"timestamp\":\"2026-03-19T02:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-newer\",\"timestamp\":\"2026-03-19T02:00:00Z\",\"cwd\":\"/tmp/app\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-19T02:00:10Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nNewer thread\"}]}}\n" +
		"{\"timestamp\":\"2026-03-19T02:00:20Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n"
	if err := os.WriteFile(newerPath, []byte(newerData), 0o600); err != nil {
		t.Fatalf("write newer: %v", err)
	}

	olderPath := filepath.Join(date18Dir, "older.jsonl")
	olderData := "" +
		"{\"timestamp\":\"2026-03-18T03:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-older\",\"timestamp\":\"2026-03-18T03:00:00Z\",\"cwd\":\"/tmp/app\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T03:00:10Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nOlder thread\"}]}}\n" +
		"{\"timestamp\":\"2026-03-18T03:00:20Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n"
	if err := os.WriteFile(olderPath, []byte(olderData), 0o600); err != nil {
		t.Fatalf("write older: %v", err)
	}

	server := newActiveTestServer(t, sessionsDir)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/active?scope=all", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if count := strings.Count(body, `class="active-date-divider-label">2026-03-19`); count != 1 {
		t.Fatalf("expected one 2026-03-19 divider, got %d body=%s", count, body)
	}
	if count := strings.Count(body, `class="active-date-divider-label">2026-03-18`); count != 1 {
		t.Fatalf("expected one 2026-03-18 divider, got %d body=%s", count, body)
	}
	if strings.Index(body, `class="active-date-divider-label">2026-03-19`) > strings.Index(body, "newer.jsonl") {
		t.Fatalf("expected 2026-03-19 divider before newer thread, body=%s", body)
	}
	if strings.Index(body, `class="active-date-divider-label">2026-03-18`) > strings.Index(body, "older.jsonl") {
		t.Fatalf("expected 2026-03-18 divider before older thread, body=%s", body)
	}
}

func TestHandleActiveFiltersByCwdAndPreservesDirectoryTabs(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	dateDir := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	appPath := filepath.Join(dateDir, "app.jsonl")
	appData := "" +
		"{\"timestamp\":\"2026-03-18T02:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-app\",\"timestamp\":\"2026-03-18T02:00:00Z\",\"cwd\":\"/tmp/app\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T02:00:10Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nKeep app thread alive\"}]}}\n" +
		"{\"timestamp\":\"2026-03-18T02:00:20Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n"
	if err := os.WriteFile(appPath, []byte(appData), 0o600); err != nil {
		t.Fatalf("write app session: %v", err)
	}

	otherPath := filepath.Join(dateDir, "other.jsonl")
	otherData := "" +
		"{\"timestamp\":\"2026-03-18T03:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-other\",\"timestamp\":\"2026-03-18T03:00:00Z\",\"cwd\":\"/tmp/other\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T03:00:10Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nKeep other thread alive\"}]}}\n" +
		"{\"timestamp\":\"2026-03-18T03:00:20Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n"
	if err := os.WriteFile(otherPath, []byte(otherData), 0o600); err != nil {
		t.Fatalf("write other session: %v", err)
	}

	server := newActiveTestServer(t, sessionsDir)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/active?scope=all&date=2026-03-18&cwd=/tmp/app", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "app.jsonl") {
		t.Fatalf("expected filtered app session, body=%s", body)
	}
	if strings.Contains(body, "other.jsonl") {
		t.Fatalf("expected other cwd session to be filtered out, body=%s", body)
	}
	if !strings.Contains(body, "Directory filter active: /tmp/app") {
		t.Fatalf("expected active page cwd notice, body=%s", body)
	}
	if !strings.Contains(body, `href="/2026/03/18/?cwd=%2Ftmp%2Fapp"`) {
		t.Fatalf("expected by-day tab to link back to cwd day view, body=%s", body)
	}
	if !strings.Contains(body, `href="/active?cwd=%2Ftmp%2Fapp&amp;date=2026-03-18&amp;scope=all"`) {
		t.Fatalf("expected all-active tab to preserve cwd/date, body=%s", body)
	}
	if !strings.Contains(body, `href="/active?cwd=%2Ftmp%2Fapp&amp;date=2026-03-18&amp;scope=ended"`) {
		t.Fatalf("expected ended tab to preserve cwd/date, body=%s", body)
	}
}

func TestHandleActiveStateMarksEndedAndReopens(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(filepath.Join(sessionsDir, "2026", "03", "18"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	filePath := filepath.Join(sessionsDir, "2026", "03", "18", "ended.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-18T03:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-2\",\"timestamp\":\"2026-03-18T03:00:00Z\",\"cwd\":\"/tmp/app\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T03:00:10Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nReview it\"}]}}\n" +
		"{\"timestamp\":\"2026-03-18T03:00:20Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n"
	if err := os.WriteFile(filePath, []byte(data), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	server := newActiveTestServer(t, sessionsDir)
	summary := server.active.Summaries()[0]

	postReq := httptest.NewRequest(http.MethodPost, "http://example.com/active/state", strings.NewReader("action=end&key="+url.QueryEscape(summary.Key)))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRec := httptest.NewRecorder()
	server.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", postRec.Code, postRec.Body.String())
	}
	if got := len(server.activeState.Snapshot()); got != 1 {
		t.Fatalf("expected one ended mark, got %d", got)
	}

	endedReq := httptest.NewRequest(http.MethodGet, "http://example.com/active?scope=ended", nil)
	endedRec := httptest.NewRecorder()
	server.ServeHTTP(endedRec, endedReq)
	if body := endedRec.Body.String(); !strings.Contains(body, "↩️ Reopen") || !strings.Contains(body, "ended.jsonl") {
		t.Fatalf("expected ended thread to render, body=%s", body)
	}

	reopenReq := httptest.NewRequest(http.MethodPost, "http://example.com/active/state", strings.NewReader("action=reopen&key="+url.QueryEscape(summary.Key)))
	reopenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reopenRec := httptest.NewRecorder()
	server.ServeHTTP(reopenRec, reopenReq)

	if reopenRec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", reopenRec.Code, reopenRec.Body.String())
	}
	if got := len(server.activeState.Snapshot()); got != 0 {
		t.Fatalf("expected ended mark to clear, got %d", got)
	}
}

func TestHandleSessionShowsThreadStateActionAndEndedState(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(filepath.Join(sessionsDir, "2026", "03", "18"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	filePath := filepath.Join(sessionsDir, "2026", "03", "18", "thread-state.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-03-18T04:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-3\",\"timestamp\":\"2026-03-18T04:00:00Z\",\"cwd\":\"/tmp/app\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T04:00:10Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nHandle it\"}]}}\n" +
		"{\"timestamp\":\"2026-03-18T04:00:20Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n"
	if err := os.WriteFile(filePath, []byte(data), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	server := newActiveTestServer(t, sessionsDir)
	summary := server.active.Summaries()[0]

	sessionReq := httptest.NewRequest(http.MethodGet, "http://example.com/2026/03/18/thread-state.jsonl", nil)
	sessionRec := httptest.NewRecorder()
	server.ServeHTTP(sessionRec, sessionReq)

	if sessionRec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", sessionRec.Code, sessionRec.Body.String())
	}
	if body := sessionRec.Body.String(); !strings.Contains(body, "▶️ Copy resume command") {
		t.Fatalf("expected resume command action, body=%s", body)
	} else if !strings.Contains(body, "⏹️ End") {
		t.Fatalf("expected end action on session page, body=%s", body)
	} else if !strings.Contains(body, "Waiting for user") {
		t.Fatalf("expected waiting-user status on session page, body=%s", body)
	} else if !strings.Contains(body, "STATE:") {
		t.Fatalf("expected state label on session page, body=%s", body)
	} else if !strings.Contains(body, "Navigate this thread:") {
		t.Fatalf("expected navigate label on session page, body=%s", body)
	} else if !strings.Contains(body, "Previous user message") || !strings.Contains(body, "Next user message") || !strings.Contains(body, "Last user message") {
		t.Fatalf("expected user jump controls on session page, body=%s", body)
	} else if !strings.Contains(body, `data-active-key="`+summary.Key+`"`) {
		t.Fatalf("expected active key on session page, body=%s", body)
	}

	postReq := httptest.NewRequest(http.MethodPost, "http://example.com/active/state", strings.NewReader("action=end&key="+url.QueryEscape(summary.Key)))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRec := httptest.NewRecorder()
	server.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", postRec.Code, postRec.Body.String())
	}

	endedReq := httptest.NewRequest(http.MethodGet, "http://example.com/2026/03/18/thread-state.jsonl", nil)
	endedRec := httptest.NewRecorder()
	server.ServeHTTP(endedRec, endedReq)

	if endedRec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", endedRec.Code, endedRec.Body.String())
	}
	if body := endedRec.Body.String(); !strings.Contains(body, "↩️ Reopen") {
		t.Fatalf("expected reopen action on ended session page, body=%s", body)
	} else if !strings.Contains(body, "Ended") {
		t.Fatalf("expected ended status on session page, body=%s", body)
	}
}

func TestHandleDayShowsThreadStateActionsForDirectorySessions(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	dateDir := filepath.Join(sessionsDir, "2026", "03", "18")
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	waitingUserPath := filepath.Join(dateDir, "waiting-user.jsonl")
	waitingUserData := "" +
		"{\"timestamp\":\"2026-03-18T05:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-user\",\"timestamp\":\"2026-03-18T05:00:00Z\",\"cwd\":\"/tmp/app\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T05:00:10Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nWrap it up\"}]}}\n" +
		"{\"timestamp\":\"2026-03-18T05:00:20Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n"
	if err := os.WriteFile(waitingUserPath, []byte(waitingUserData), 0o600); err != nil {
		t.Fatalf("write waiting-user: %v", err)
	}

	waitingAgentPath := filepath.Join(dateDir, "waiting-agent.jsonl")
	waitingAgentData := "" +
		"{\"timestamp\":\"2026-03-18T06:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-agent\",\"timestamp\":\"2026-03-18T06:00:00Z\",\"cwd\":\"/tmp/app\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-03-18T06:00:10Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"## My request for Codex:\\nKeep working\"}]}}\n" +
		"{\"timestamp\":\"2026-03-18T06:00:20Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\"}}\n"
	if err := os.WriteFile(waitingAgentPath, []byte(waitingAgentData), 0o600); err != nil {
		t.Fatalf("write waiting-agent: %v", err)
	}

	server := newActiveTestServer(t, sessionsDir)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/2026/03/18/?cwd=/tmp/app", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "waiting-user.jsonl") {
		t.Fatalf("expected waiting-user session, body=%s", body)
	}
	if !strings.Contains(body, "waiting-agent.jsonl") {
		t.Fatalf("expected waiting-agent session, body=%s", body)
	}
	if !strings.Contains(body, "Waiting for user") {
		t.Fatalf("expected waiting-user status, body=%s", body)
	}
	if !strings.Contains(body, "Waiting for agent") {
		t.Fatalf("expected waiting-agent status, body=%s", body)
	}
	if !strings.Contains(body, "thread-status-waiting-user") {
		t.Fatalf("expected waiting-user card class on day page, body=%s", body)
	}
	if !strings.Contains(body, "⏹️ End") {
		t.Fatalf("expected end action button, body=%s", body)
	}
	if !strings.Contains(body, `href="/2026/03/18/?cwd=%2Ftmp%2Fapp"`) {
		t.Fatalf("expected by-day tab on day page, body=%s", body)
	}
	if !strings.Contains(body, `href="/active?cwd=%2Ftmp%2Fapp&amp;date=2026-03-18&amp;scope=all"`) {
		t.Fatalf("expected all-active tab on day page, body=%s", body)
	}
	if !strings.Contains(body, `href="/active?cwd=%2Ftmp%2Fapp&amp;date=2026-03-18&amp;scope=ended"`) {
		t.Fatalf("expected ended tab on day page, body=%s", body)
	}

	var waitingUserSummary active.Summary
	foundWaitingUser := false
	for _, summary := range server.active.Summaries() {
		if summary.SessionID == "session-user" {
			waitingUserSummary = summary
			foundWaitingUser = true
			break
		}
	}
	if !foundWaitingUser {
		t.Fatal("expected waiting-user summary")
	}

	postReq := httptest.NewRequest(http.MethodPost, "http://example.com/active/state", strings.NewReader("action=end&key="+url.QueryEscape(waitingUserSummary.Key)))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRec := httptest.NewRecorder()
	server.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", postRec.Code, postRec.Body.String())
	}

	endedReq := httptest.NewRequest(http.MethodGet, "http://example.com/2026/03/18/?cwd=/tmp/app", nil)
	endedRec := httptest.NewRecorder()
	server.ServeHTTP(endedRec, endedReq)

	if endedRec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", endedRec.Code, endedRec.Body.String())
	}
	endedBody := endedRec.Body.String()
	if !strings.Contains(endedBody, "Ended") {
		t.Fatalf("expected ended status on day page, body=%s", endedBody)
	}
	if !strings.Contains(endedBody, "↩️ Reopen") {
		t.Fatalf("expected reopen action on day page, body=%s", endedBody)
	}
}

func newActiveTestServer(t *testing.T, sessionsDir string) *Server {
	t.Helper()

	idx := sessions.NewIndex(sessionsDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh sessions: %v", err)
	}

	activeIdx := active.NewIndex()
	if err := activeIdx.RefreshFrom(idx); err != nil {
		t.Fatalf("refresh active index: %v", err)
	}

	state, err := active.LoadStateStore(filepath.Join(filepath.Dir(sessionsDir), "session_state.json"))
	if err != nil {
		t.Fatalf("load state: %v", err)
	}

	renderer, err := render.New()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	server := NewServer(idx, nil, renderer, sessionsDir, "", "", 3)
	server.EnableActive(activeIdx, state, time.Hour)
	return server
}
