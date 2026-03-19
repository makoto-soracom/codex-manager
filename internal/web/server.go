package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"codex-manager/internal/active"
	"codex-manager/internal/notifications"
	"codex-manager/internal/render"
	"codex-manager/internal/repooverride"
	"codex-manager/internal/search"
	"codex-manager/internal/sessions"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

type htmlBucketUploader interface {
	Upload(ctx context.Context, html string) (string, error)
}

const activeTimeZoneCookie = "codex_tz"
const hookMaxBodyBytes = 1 << 20

// Server serves the HTML views.
type Server struct {
	idx                 *sessions.Index
	search              *search.Index
	active              *active.Index
	activeState         *active.StateStore
	notifications       *notifications.Store
	repoOverrides       *repooverride.Store
	renderer            *render.Renderer
	sessionsDir         string
	shareDir            string
	shareAddr           string
	themeClass          string
	useTailscale        bool
	tailscaleHost       string
	htmlBucket          htmlBucketUploader
	activeRefreshMaxAge time.Duration
	activeRefreshMu     sync.Mutex
}

// NewServer wires up the HTTP server.
func NewServer(idx *sessions.Index, searchIdx *search.Index, renderer *render.Renderer, sessionsDir, shareDir, shareAddr string, theme int) *Server {
	return &Server{
		idx:                 idx,
		search:              searchIdx,
		renderer:            renderer,
		sessionsDir:         sessionsDir,
		shareDir:            shareDir,
		shareAddr:           shareAddr,
		themeClass:          themeClass(theme),
		activeRefreshMaxAge: 15 * time.Second,
	}
}

// EnableTailscale sets the host used for share redirects.
func (s *Server) EnableTailscale(host string) {
	s.useTailscale = true
	s.tailscaleHost = strings.TrimSuffix(host, ".")
}

// EnableHTMLBucket configures htmlbucket as the active share backend.
func (s *Server) EnableHTMLBucket(client htmlBucketUploader) {
	s.htmlBucket = client
}

// EnableActive configures active-thread summaries and persisted state.
func (s *Server) EnableActive(activeIdx *active.Index, state *active.StateStore, refreshMaxAge time.Duration) {
	s.active = activeIdx
	s.activeState = state
	if refreshMaxAge > 0 {
		s.activeRefreshMaxAge = refreshMaxAge
	}
}

// EnableNotifications configures the webhook notification log.
func (s *Server) EnableNotifications(store *notifications.Store) {
	s.notifications = store
}

// EnableRepoOverrides configures repository URL overrides keyed by cwd prefix.
func (s *Server) EnableRepoOverrides(store *repooverride.Store) {
	s.repoOverrides = store
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	pathValue := strings.Trim(r.URL.Path, "/")
	if pathValue == "" {
		s.handleIndex(w, r)
		return
	}
	if pathValue == "dir" {
		s.handleDir(w, r)
		return
	}
	if pathValue == "active" {
		s.handleActive(w, r)
		return
	}
	if pathValue == "active/state" {
		s.handleActiveState(w, r)
		return
	}
	if pathValue == "hook" {
		s.handleHook(w, r)
		return
	}
	if pathValue == "notifications" {
		s.handleNotifications(w, r)
		return
	}
	if pathValue == "search" {
		s.handleSearch(w, r)
		return
	}
	if strings.HasPrefix(pathValue, "markdown/") {
		s.handleSessionMarkdown(w, r, strings.TrimPrefix(pathValue, "markdown/"))
		return
	}
	if strings.HasPrefix(pathValue, "raw/") {
		s.handleRaw(w, r, strings.TrimPrefix(pathValue, "raw/"))
		return
	}

	parts := strings.Split(pathValue, "/")
	if len(parts) == 5 && r.Method == http.MethodPost && parts[0] == "share" {
		s.handleShare(w, r, parts[1:])
		return
	}
	if len(parts) == 3 {
		s.handleDay(w, r, parts)
		return
	}
	if len(parts) == 4 {
		s.handleSession(w, r, parts)
		return
	}

	http.NotFound(w, r)
}

type dateView struct {
	Label string
	Path  string
	Count int
}

type dirView struct {
	Label       string
	Value       string
	Count       int
	RecentCount int
	HeatColor   template.CSS
}

type sessionView struct {
	Name                      string
	DisplayName               string
	Size                      string
	ModTime                   string
	ModTimeOnly               string
	ResumeCommand             string
	Cwd                       string
	Branch                    string
	BranchURL                 string
	DateLabel                 string
	DatePath                  string
	ThreadStateKey            string
	ThreadStatusLabel         string
	ThreadStatusClass         string
	ThreadAction              string
	ThreadActionLabel         string
	LastUserSnippet           string
	LastUserSnippetTitle      string
	LastUserSnippetClass      string
	LastAssistantSnippet      string
	LastAssistantSnippetTitle string
	LastAssistantSnippetClass string
}

type indexView struct {
	Dates       []dateView
	Dirs        []dirView
	SessionsDir string
	LastScan    string
	View        string
	HeatMode    string
	ThemeClass  string
}

type dayView struct {
	Date             dateView
	Sessions         []sessionView
	Dirs             []dirView
	SelectedCwd      string
	SelectedCwdLabel string
	ActiveTabs       []activeTabView
	FallbackDate     *dateView
	FallbackSessions []sessionView
	FallbackDirs     []dirView
	Page             int
	TotalPages       int
	HasPrev          bool
	HasNext          bool
	PrevPage         int
	NextPage         int
	ShowAll          bool
	View             string
	ThemeClass       string
}

type dirPageView struct {
	Dir        dirView
	Dates      []dateView
	Sessions   []sessionView
	Page       int
	TotalPages int
	HasPrev    bool
	HasNext    bool
	PrevPage   int
	NextPage   int
	ShowAll    bool
	ThemeClass string
}

type sessionPageView struct {
	Date                dateView
	File                sessionView
	Meta                *sessions.SessionMeta
	IsSubagentThread    bool
	SubagentDisplayName string
	SubagentDisplayRole string
	ParentThreadID      string
	ParentSessionPath   string
	ParentSessionTitle  string
	UserNavLabel        string
	Items               []itemView
	ResumeCommand       string
	ThreadStateKey      string
	ThreadStatusLabel   string
	ThreadStatusClass   string
	ThreadAction        string
	ThreadActionLabel   string
	ThemeClass          string
	IsJSONL             bool
	LastUserLine        int
	LastAgentLine       int
	LastItemLine        int
}

type activeTabView struct {
	Label  string
	Path   string
	Active bool
}

type activeSessionView struct {
	Key                       string
	DisplayName               string
	DetailPath                string
	DateLabel                 string
	ShowDateDivider           bool
	Cwd                       string
	Branch                    string
	LastActivity              string
	ResumeCommand             string
	HasResumeCommand          bool
	StatusLabel               string
	StatusClass               string
	Action                    string
	ActionLabel               string
	LastUserSnippet           string
	LastUserSnippetTitle      string
	LastUserSnippetClass      string
	LastAssistantSnippet      string
	LastAssistantSnippetTitle string
	LastAssistantSnippetClass string
}

type activePageView struct {
	Heading          string
	Scope            string
	Tabs             []activeTabView
	Threads          []activeSessionView
	EmptyMessage     string
	ThreadCount      int
	ShowDayNav       bool
	SelectedDate     string
	SelectedCwd      string
	SelectedCwdLabel string
	PrevDayPath      string
	NextDayPath      string
	LastScan         string
	TimeZone         string
	ThemeClass       string
}

type notificationHeaderView struct {
	Key   string
	Value string
}

type notificationEntryView struct {
	ID          string
	ReceivedAt  string
	Method      string
	Path        string
	ContentType string
	UserAgent   string
	RemoteAddr  string
	SizeLabel   string
	Preview     string
	Body        string
	RawBody     string
	IsJSON      bool
	Headers     []notificationHeaderView
}

type notificationsPageView struct {
	Entries      []notificationEntryView
	LastScan     string
	ThemeClass   string
	EmptyMessage string
}

type itemView struct {
	Line                 int
	Timestamp            string
	Type                 string
	Subtype              string
	Role                 string
	RoleLabel            string
	SpeakerClass         string
	Title                string
	Content              string
	Class                string
	AutoCtx              bool
	IsTurnAborted        bool
	TurnAbortedMessage   string
	SpeakerName          string
	SpeakerRole          string
	SubagentID           string
	SubagentNickname     string
	SubagentStatusType   string
	SubagentRequest      string
	SubagentSessionPath  string
	SubagentSessionTitle string
	Markdown             string
	SubagentRequestHTML  template.HTML
	HTML                 template.HTML
	ToolRunCallTitle     string
	ToolRunOutputLine    int
	ToolRunOutputTitle   string
	ToolRunOutputHTML    template.HTML
	ToolRunOutputTime    string
}

type updatePlanHTMLArgs struct {
	Explanation string               `json:"explanation"`
	Plan        []updatePlanHTMLStep `json:"plan"`
}

type updatePlanHTMLStep struct {
	Status string `json:"status"`
	Step   string `json:"step"`
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	view := r.URL.Query().Get("view")
	heat := r.URL.Query().Get("heat")
	if view == "" && r.URL.RawQuery == "" {
		view = "dir"
		heat = "1h"
	} else if view != "dir" {
		view = "date"
	}

	indexView := s.buildIndexView(view, heat)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.renderer.Execute(w, "index", indexView)
}

func (s *Server) handleDir(w http.ResponseWriter, r *http.Request) {
	cwd := normalizeCwdParam(r.URL.Query().Get("cwd"))
	if cwd == "" {
		indexView := s.buildIndexView("dir", r.URL.Query().Get("heat"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = s.renderer.Execute(w, "index", indexView)
		return
	}

	files := s.idx.SessionsByCwd(cwd)
	counts := make(map[sessions.DateKey]int, len(files))
	for _, file := range files {
		counts[file.Date]++
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].ModTime.Equal(files[j].ModTime) {
			dateI := files[i].Date.String()
			dateJ := files[j].Date.String()
			if dateI != dateJ {
				return dateI > dateJ
			}
			return files[i].Name > files[j].Name
		}
		return files[i].ModTime.After(files[j].ModTime)
	})
	page := parsePageParam(r)
	showAll := parseBoolParam(r, "all")
	sessionsView, pager := s.buildSessionViewsPage(files, page, 10, showAll)

	dates := s.idx.Dates()
	dateViews := make([]dateView, 0, len(counts))
	for _, date := range dates {
		if count, ok := counts[date]; ok {
			dateViews = append(dateViews, dateView{
				Label: date.String(),
				Path:  date.Path(),
				Count: count,
			})
		}
	}

	dir := dirView{
		Label: dirLabel(cwd),
		Value: cwd,
		Count: len(files),
	}

	view := dirPageView{
		Dir:        dir,
		Dates:      dateViews,
		Sessions:   sessionsView,
		Page:       pager.Page,
		TotalPages: pager.TotalPages,
		HasPrev:    pager.HasPrev,
		HasNext:    pager.HasNext,
		PrevPage:   pager.PrevPage,
		NextPage:   pager.NextPage,
		ShowAll:    showAll,
		ThemeClass: s.themeClass,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.renderer.Execute(w, "dir", view)
}

func (s *Server) handleActive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	if s.active == nil {
		http.Error(w, "active index not available", http.StatusServiceUnavailable)
		return
	}
	if err := s.refreshActiveIfStale(); err != nil {
		log.Printf("active refresh failed: %v", err)
	}

	view := s.buildActivePageView(r)
	templateName := "active"
	if parseBoolParam(r, "partial") {
		templateName = "active_content"
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.renderer.Execute(w, templateName, view)
}

func (s *Server) handleActiveState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if s.active == nil || s.activeState == nil {
		http.Error(w, "active state not available", http.StatusServiceUnavailable)
		return
	}
	if err := s.refreshActiveIfStale(); err != nil {
		log.Printf("active refresh failed before state change: %v", err)
	}
	if err := r.ParseForm(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid form payload")
		return
	}

	action := strings.TrimSpace(r.Form.Get("action"))
	key := strings.TrimSpace(r.Form.Get("key"))
	if key == "" {
		writeJSONError(w, http.StatusBadRequest, "missing thread key")
		return
	}

	var err error
	switch action {
	case "end":
		summary, ok := s.active.Lookup(key)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "thread not found")
			return
		}
		err = s.activeState.MarkEnded(key, summary.ActivityToken)
	case "reopen":
		err = s.activeState.Reopen(key)
	default:
		writeJSONError(w, http.StatusBadRequest, "invalid action")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handleHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if s.notifications == nil {
		http.Error(w, "notification store not available", http.StatusServiceUnavailable)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, hookMaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	headers := make(map[string][]string, len(r.Header))
	for key, values := range r.Header {
		headers[key] = append([]string(nil), values...)
	}
	if _, err := s.notifications.AppendRequest(r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.UserAgent(), r.RemoteAddr, headers, body); err != nil {
		http.Error(w, "failed to store notification", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	if s.notifications == nil {
		http.Error(w, "notification store not available", http.StatusServiceUnavailable)
		return
	}

	view := s.buildNotificationsPageView(r)
	templateName := "notifications"
	if parseBoolParam(r, "partial") {
		templateName = "notifications_content"
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.renderer.Execute(w, templateName, view)
}

func (s *Server) handleDay(w http.ResponseWriter, r *http.Request, parts []string) {
	date, ok := sessions.ParseDate(parts[0], parts[1], parts[2])
	if !ok {
		http.NotFound(w, r)
		return
	}
	if s.active != nil {
		if err := s.refreshActiveIfStale(); err != nil {
			log.Printf("active refresh failed during day render: %v", err)
		}
	}
	selectedCwd := normalizeCwdParam(r.URL.Query().Get("cwd"))
	viewMode := strings.TrimSpace(r.URL.Query().Get("view"))
	if viewMode != "dir" {
		viewMode = "sessions"
	}

	requestedFiles := s.idx.SessionsByDate(date)
	filtered := filterSessionFilesByCwd(requestedFiles, selectedCwd)
	requestedViews := []sessionView{}
	dirViews := buildDirViewsFromFiles(requestedFiles)
	page := parsePageParam(r)
	showAll := parseBoolParam(r, "all")
	pager := paginationInfo{Page: page}
	var fallbackDate *dateView
	var fallbackSessions []sessionView
	var fallbackDirs []dirView
	if selectedCwd != "" && len(filtered) == 0 {
		if prevDate, ok := previousDateKey(date); ok {
			prevFiles := s.idx.SessionsByDate(prevDate)
			prevFiltered := filterSessionFilesByCwd(prevFiles, selectedCwd)
			if len(prevFiltered) > 0 {
				fallbackDate = &dateView{
					Label: prevDate.String(),
					Path:  prevDate.Path(),
					Count: len(prevFiles),
				}
				pageSessions, pageInfo := s.buildSessionViewsPage(prevFiltered, page, 10, showAll)
				pager = pageInfo
				fallbackSessions = pageSessions
				fallbackDirs = buildDirViewsFromFiles(prevFiles)
			}
		}
	}
	if fallbackDate == nil {
		pageSessions, pageInfo := s.buildSessionViewsPage(filtered, page, 10, showAll)
		pager = pageInfo
		requestedViews = pageSessions
	}

	selectedLabel := ""
	if selectedCwd != "" {
		selectedLabel = dirLabel(selectedCwd)
	}
	var activeTabs []activeTabView
	if selectedCwd != "" && s.active != nil {
		activeTabs = buildActiveTabs("day", date.String(), time.Now().Format("2006-01-02"), selectedCwd)
	}

	view := dayView{
		Date: dateView{
			Label: date.String(),
			Path:  date.Path(),
			Count: len(requestedFiles),
		},
		Sessions:         requestedViews,
		Dirs:             dirViews,
		SelectedCwd:      selectedCwd,
		SelectedCwdLabel: selectedLabel,
		ActiveTabs:       activeTabs,
		FallbackDate:     fallbackDate,
		FallbackSessions: fallbackSessions,
		FallbackDirs:     fallbackDirs,
		Page:             pager.Page,
		TotalPages:       pager.TotalPages,
		HasPrev:          pager.HasPrev,
		HasNext:          pager.HasNext,
		PrevPage:         pager.PrevPage,
		NextPage:         pager.NextPage,
		ShowAll:          showAll,
		View:             viewMode,
		ThemeClass:       s.themeClass,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.renderer.Execute(w, "day", view)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request, parts []string) {
	if s.active != nil {
		if err := s.refreshActiveIfStale(); err != nil {
			log.Printf("active refresh failed during session render: %v", err)
		}
	}
	view, err := s.buildSessionView(parts)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.renderer.Execute(w, "session", view)
}

func (s *Server) handleSessionMarkdown(w http.ResponseWriter, r *http.Request, path string) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	parts := strings.Split(path, "/")
	if len(parts) != 4 {
		http.NotFound(w, r)
		return
	}

	view, err := s.buildSessionView(parts)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	line := 0
	if rawLine := strings.TrimSpace(r.URL.Query().Get("line")); rawLine != "" {
		parsed, err := strconv.Atoi(rawLine)
		if err != nil || parsed <= 0 {
			http.Error(w, "invalid line", http.StatusBadRequest)
			return
		}
		line = parsed
	}

	var markdown string
	if line > 0 {
		var ok bool
		markdown, ok = sessionItemMarkdown(view.Items, line)
		if !ok {
			http.NotFound(w, r)
			return
		}
	} else {
		markdown = joinItemMarkdown(view.Items)
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, markdown)
}

type searchResponse struct {
	Query   string          `json:"query"`
	Results []search.Result `json:"results"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	if s.search == nil {
		http.Error(w, "search index not available", http.StatusServiceUnavailable)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("query"))
	cwdFilter := normalizeSearchCwdFilter(r.URL.Query().Get("cwd"))
	limit := 50
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 200 {
		limit = 200
	}

	var results []search.Result
	if len(query) >= 2 {
		results = s.search.SearchWithCwd(query, limit, cwdFilter)
	} else {
		results = []search.Result{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(searchResponse{Query: query, Results: results})
}

func (s *Server) handleShare(w http.ResponseWriter, r *http.Request, parts []string) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	view, err := s.buildSessionView(parts)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var buf bytes.Buffer
	if err := s.renderer.Execute(&buf, "session", view); err != nil {
		http.Error(w, fmt.Sprintf("failed to render html: %v", err), http.StatusInternalServerError)
		return
	}

	if s.htmlBucket != nil {
		shareURL, err := s.htmlBucket.Upload(r.Context(), buf.String())
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("htmlbucket upload failed: %v", err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"url": shareURL})
		return
	}

	if err := os.MkdirAll(s.shareDir, 0o700); err != nil {
		http.Error(w, fmt.Sprintf("failed to create share dir: %v", err), http.StatusInternalServerError)
		return
	}

	token, err := randomToken(16)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create share token: %v", err), http.StatusInternalServerError)
		return
	}

	fileName := formatUUID(token) + ".html"
	targetFile := filepath.Join(s.shareDir, fileName)
	if err := os.WriteFile(targetFile, buf.Bytes(), 0o600); err != nil {
		http.Error(w, fmt.Sprintf("failed to write share file: %v", err), http.StatusInternalServerError)
		return
	}

	shareURL := s.buildShareURL(r, fileName)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"url": shareURL})
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request, rawPath string) {
	parts := strings.Split(strings.Trim(rawPath, "/"), "/")
	if len(parts) != 4 {
		http.NotFound(w, r)
		return
	}
	date, ok := sessions.ParseDate(parts[0], parts[1], parts[2])
	if !ok {
		http.NotFound(w, r)
		return
	}
	filename := parts[3]
	if filename == "" || strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		http.NotFound(w, r)
		return
	}
	file, ok := s.idx.Lookup(date, filename)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", file.Name))
	http.ServeFile(w, r, file.Path)
}

func formatBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div := float64(size)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, suffix := range units {
		div = div / unit
		if div < unit {
			return fmt.Sprintf("%.1f %s", div, suffix)
		}
	}
	return fmt.Sprintf("%.1f PB", div/unit)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}

func formatTimeOnly(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("15:04:05")
}

func formatScanTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format(time.RFC3339)
}

type paginationInfo struct {
	Page       int
	TotalPages int
	HasPrev    bool
	HasNext    bool
	PrevPage   int
	NextPage   int
}

func parsePageParam(r *http.Request) int {
	page := 1
	if rawPage := r.URL.Query().Get("page"); rawPage != "" {
		if parsed, err := strconv.Atoi(rawPage); err == nil {
			page = parsed
		}
	}
	if page < 1 {
		page = 1
	}
	return page
}

func parseBoolParam(r *http.Request, key string) bool {
	value := strings.TrimSpace(strings.ToLower(r.URL.Query().Get(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func paginateSessionFiles(files []sessions.SessionFile, page int, perPage int) ([]sessions.SessionFile, paginationInfo) {
	info := paginationInfo{Page: page}
	total := len(files)
	if perPage <= 0 {
		info.TotalPages = 1
		info.HasPrev = false
		info.HasNext = false
		info.PrevPage = 1
		info.NextPage = 1
		return files, info
	}
	if total == 0 {
		info.Page = 1
		info.TotalPages = 0
		info.PrevPage = 1
		info.NextPage = 1
		return nil, info
	}
	totalPages := (total + perPage - 1) / perPage
	if page > totalPages {
		page = totalPages
	}
	if page < 1 {
		page = 1
	}
	start := (page - 1) * perPage
	end := start + perPage
	if start < 0 {
		start = 0
	}
	if end > total {
		end = total
	}
	pageFiles := files[start:end]
	info.Page = page
	info.TotalPages = totalPages
	info.HasPrev = page > 1
	info.HasNext = page < totalPages
	info.PrevPage = 1
	info.NextPage = totalPages
	if info.HasPrev {
		info.PrevPage = page - 1
	}
	if info.HasNext {
		info.NextPage = page + 1
	}
	return pageFiles, info
}

func (s *Server) buildIndexView(view string, heatMode string) indexView {
	heatMode = parseHeatMode(heatMode)
	dates := s.idx.Dates()
	dateViews := make([]dateView, 0, len(dates))
	for _, date := range dates {
		files := s.idx.SessionsByDate(date)
		dateViews = append(dateViews, dateView{
			Label: date.String(),
			Path:  date.Path(),
			Count: len(files),
		})
	}

	recentCounts := map[string]int{}
	recentMax := 0
	if view == "dir" {
		now := time.Now()
		since := now.AddDate(0, 0, -7)
		allowFallback := true
		switch heatMode {
		case "today":
			since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			allowFallback = false
		case "1h":
			since = now.Add(-1 * time.Hour)
			allowFallback = false
		}
		recentCounts, recentMax = s.recentCwdCounts(since)
		if allowFallback && recentMax == 0 {
			recentCounts, recentMax = s.recentCwdCountsFromLatestDates(7)
		}
	}
	dirViews := buildDirViewsFromCounts(s.idx.CwdCounts(), recentCounts, recentMax, view == "dir")
	lastScan := s.idx.LastUpdated()

	return indexView{
		Dates:       dateViews,
		Dirs:        dirViews,
		SessionsDir: s.sessionsDir,
		LastScan:    formatScanTime(lastScan),
		View:        view,
		HeatMode:    heatMode,
		ThemeClass:  s.themeClass,
	}
}

func (s *Server) refreshActiveIfStale() error {
	if s.active == nil || s.idx == nil {
		return nil
	}
	maxAge := s.activeRefreshMaxAge
	if maxAge <= 0 {
		maxAge = 15 * time.Second
	}
	if time.Since(s.idx.LastUpdated()) < maxAge && time.Since(s.active.LastUpdated()) < maxAge {
		return nil
	}

	s.activeRefreshMu.Lock()
	defer s.activeRefreshMu.Unlock()

	if time.Since(s.idx.LastUpdated()) < maxAge && time.Since(s.active.LastUpdated()) < maxAge {
		return nil
	}
	if err := s.idx.Refresh(); err != nil {
		return err
	}
	if err := s.active.RefreshFrom(s.idx); err != nil {
		return err
	}
	if s.activeState != nil {
		if err := s.activeState.Reconcile(s.active.Summaries()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) buildActivePageView(r *http.Request) activePageView {
	loc, tz := activeLocation(r)
	scope := parseActiveScope(r)
	selectedDate := parseActiveDate(r, loc)
	selectedDateLabel := selectedDate.Format("2006-01-02")
	todayLabel := startOfDay(time.Now().In(loc)).Format("2006-01-02")
	selectedCwd := normalizeCwdParam(r.URL.Query().Get("cwd"))
	selectedCwdLabel := ""
	if selectedCwd != "" {
		selectedCwdLabel = dirLabel(selectedCwd)
	}

	summaries := []active.Summary{}
	if s.active != nil {
		summaries = s.active.Summaries()
	}
	if s.activeState != nil {
		if err := s.activeState.Reconcile(summaries); err != nil {
			log.Printf("active state reconcile failed during render: %v", err)
		}
	}
	endedMarks := map[string]active.EndedMark{}
	if s.activeState != nil {
		endedMarks = s.activeState.Snapshot()
	}

	threads := make([]activeSessionView, 0, len(summaries))
	for _, summary := range summaries {
		ended := false
		if mark, ok := endedMarks[summary.Key]; ok {
			ended = mark.ActivityToken == "" || mark.ActivityToken == summary.ActivityToken
		}
		if !matchesActiveCwd(summary, selectedCwd) {
			continue
		}
		if !matchesActiveScope(summary, scope, selectedDateLabel, loc, ended) {
			continue
		}
		threads = append(threads, buildActiveSessionRow(summary, ended, loc))
	}
	if scope != "day" {
		lastDateLabel := ""
		for i := range threads {
			if threads[i].DateLabel != lastDateLabel {
				threads[i].ShowDateDivider = true
				lastDateLabel = threads[i].DateLabel
			}
		}
	}

	heading, emptyMessage := activeHeading(scope, selectedDateLabel, todayLabel)
	prevDayPath := ""
	nextDayPath := ""
	if scope == "day" {
		prevDayPath = buildActivePagePath("day", selectedDate.AddDate(0, 0, -1).Format("2006-01-02"), todayLabel, selectedCwd)
		nextDayPath = buildActivePagePath("day", selectedDate.AddDate(0, 0, 1).Format("2006-01-02"), todayLabel, selectedCwd)
	}

	return activePageView{
		Heading:          heading,
		Scope:            scope,
		Tabs:             buildActiveTabs(scope, selectedDateLabel, todayLabel, selectedCwd),
		Threads:          threads,
		EmptyMessage:     emptyMessage,
		ThreadCount:      len(threads),
		ShowDayNav:       scope == "day",
		SelectedDate:     selectedDateLabel,
		SelectedCwd:      selectedCwd,
		SelectedCwdLabel: selectedCwdLabel,
		PrevDayPath:      prevDayPath,
		NextDayPath:      nextDayPath,
		LastScan:         formatScanTime(maxTime(s.idx.LastUpdated(), s.active.LastUpdated())),
		TimeZone:         tz,
		ThemeClass:       s.themeClass,
	}
}

func (s *Server) buildNotificationsPageView(r *http.Request) notificationsPageView {
	loc, _ := activeLocation(r)
	rawEntries := []notifications.Entry{}
	if s.notifications != nil {
		rawEntries = s.notifications.Entries()
	}
	entries := make([]notificationEntryView, 0, len(rawEntries))
	for _, entry := range rawEntries {
		headers := make([]notificationHeaderView, 0, len(entry.Headers))
		headerKeys := make([]string, 0, len(entry.Headers))
		for key := range entry.Headers {
			headerKeys = append(headerKeys, key)
		}
		sort.Strings(headerKeys)
		for _, key := range headerKeys {
			headers = append(headers, notificationHeaderView{
				Key:   key,
				Value: strings.Join(entry.Headers[key], ", "),
			})
		}

		body := entry.PrettyBody
		if body == "" {
			body = entry.Body
		}
		entries = append(entries, notificationEntryView{
			ID:          entry.ID,
			ReceivedAt:  formatTime(entry.ReceivedAt.In(loc)),
			Method:      entry.Method,
			Path:        entry.Path,
			ContentType: entry.ContentType,
			UserAgent:   entry.UserAgent,
			RemoteAddr:  entry.RemoteAddr,
			SizeLabel:   formatBytes(int64(entry.Size)),
			Preview:     entry.Preview,
			Body:        body,
			RawBody:     entry.Body,
			IsJSON:      entry.IsJSON,
			Headers:     headers,
		})
	}
	lastScan := time.Now()
	return notificationsPageView{
		Entries:      entries,
		LastScan:     formatScanTime(lastScan),
		ThemeClass:   s.themeClass,
		EmptyMessage: "No notifications received yet.",
	}
}

func activeLocation(r *http.Request) (*time.Location, string) {
	if r != nil {
		if cookie, err := r.Cookie(activeTimeZoneCookie); err == nil {
			value := strings.TrimSpace(cookie.Value)
			if decoded, err := url.QueryUnescape(value); err == nil {
				value = decoded
			}
			if value != "" {
				if loc, err := time.LoadLocation(value); err == nil {
					return loc, value
				}
			}
		}
	}
	if time.Local != nil {
		return time.Local, time.Local.String()
	}
	return time.UTC, time.UTC.String()
}

func parseActiveScope(r *http.Request) string {
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	switch value {
	case "all":
		return "all"
	case "ended":
		return "ended"
	default:
		return "day"
	}
}

func parseActiveDate(r *http.Request, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	value := strings.TrimSpace(r.URL.Query().Get("date"))
	if value == "" {
		return startOfDay(time.Now().In(loc))
	}
	if parsed, err := time.ParseInLocation("2006-01-02", value, loc); err == nil {
		return startOfDay(parsed)
	}
	return startOfDay(time.Now().In(loc))
}

func startOfDay(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func activeDateString(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	if loc != nil {
		t = t.In(loc)
	}
	return t.Format("2006-01-02")
}

func matchesActiveScope(summary active.Summary, scope string, selectedDate string, loc *time.Location, ended bool) bool {
	switch scope {
	case "all":
		return !ended
	case "ended":
		return ended
	default:
		return !ended && activeDateString(summary.LastActivityAt, loc) == selectedDate
	}
}

func matchesActiveCwd(summary active.Summary, selectedCwd string) bool {
	if selectedCwd == "" {
		return true
	}
	return sessions.NormalizeCwd(summary.Cwd) == sessions.NormalizeCwd(selectedCwd)
}

func buildActiveSessionRow(summary active.Summary, ended bool, loc *time.Location) activeSessionView {
	lastActivity := summary.LastActivityAt
	if lastActivity.IsZero() {
		lastActivity = summary.ModTime
	}
	if loc != nil && !lastActivity.IsZero() {
		lastActivity = lastActivity.In(loc)
	}

	statusLabel, statusClass := activeStatus(summary.WaitState, ended)
	action := "end"
	actionLabel := "⏹️ End"
	if ended {
		action = "reopen"
		actionLabel = "↩️ Reopen"
	}

	userSnippet := summary.LastUserSnippet
	if summary.HasUserMessage && userSnippet.Text == "" {
		userSnippet.Text = "(empty)"
	}
	assistantSnippet := summary.LastAssistantSnippet

	return activeSessionView{
		Key:                       summary.Key,
		DisplayName:               summary.DisplayName,
		DetailPath:                "/" + summary.Date.Path() + "/" + summary.Name + "#last-item",
		DateLabel:                 summary.Date.String(),
		Cwd:                       displayCwd(summary.Cwd),
		Branch:                    summary.Branch,
		LastActivity:              formatTime(lastActivity),
		ResumeCommand:             summary.ResumeCommand,
		HasResumeCommand:          summary.ResumeCommand != "",
		StatusLabel:               statusLabel,
		StatusClass:               statusClass,
		Action:                    action,
		ActionLabel:               actionLabel,
		LastUserSnippet:           userSnippet.Text,
		LastUserSnippetTitle:      userSnippet.Title,
		LastUserSnippetClass:      userSnippet.SpeakerClass,
		LastAssistantSnippet:      assistantSnippet.Text,
		LastAssistantSnippetTitle: assistantSnippet.Title,
		LastAssistantSnippetClass: assistantSnippet.SpeakerClass,
	}
}

func activeStatus(waitState active.WaitState, ended bool) (string, string) {
	if ended {
		return "Ended", "ended"
	}
	switch waitState {
	case active.WaitStateAgent:
		return "Waiting for agent", "waiting-agent"
	default:
		return "Waiting for user", "waiting-user"
	}
}

func buildActiveTabs(scope string, selectedDate string, today string, selectedCwd string) []activeTabView {
	return []activeTabView{
		{Label: "By day", Path: buildByDayTabPath(selectedDate, today, selectedCwd), Active: scope == "day"},
		{Label: "All active", Path: buildActivePagePath("all", selectedDate, today, selectedCwd), Active: scope == "all"},
		{Label: "Ended", Path: buildActivePagePath("ended", selectedDate, today, selectedCwd), Active: scope == "ended"},
	}
}

func buildByDayTabPath(selectedDate string, today string, selectedCwd string) string {
	if selectedCwd != "" {
		return buildDayPagePath(selectedDate, selectedCwd)
	}
	return buildActivePagePath("day", selectedDate, today, "")
}

func buildActivePagePath(scope string, selectedDate string, today string, selectedCwd string) string {
	params := url.Values{}
	if scope != "" && scope != "day" {
		params.Set("scope", scope)
	}
	if selectedDate != "" && (selectedDate != today || selectedCwd != "") {
		params.Set("date", selectedDate)
	}
	if selectedCwd != "" {
		params.Set("cwd", selectedCwd)
	}
	return urlWithQuery("/active", params)
}

func buildDayPagePath(selectedDate string, selectedCwd string) string {
	base := "/"
	parts := strings.Split(selectedDate, "-")
	if len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != "" {
		base = "/" + strings.Join(parts, "/") + "/"
	}
	params := url.Values{}
	if selectedCwd != "" {
		params.Set("cwd", selectedCwd)
	}
	return urlWithQuery(base, params)
}

func urlWithQuery(base string, params url.Values) string {
	if len(params) == 0 {
		return base
	}
	encoded := params.Encode()
	if encoded == "" {
		return base
	}
	return base + "?" + encoded
}

func activeHeading(scope string, selectedDate string, today string) (string, string) {
	switch scope {
	case "all":
		return "All Active Threads", "No active threads found."
	case "ended":
		return "Ended Threads", "No ended threads found."
	default:
		if selectedDate == today {
			return "Today's Active Threads", "No active threads for today."
		}
		return "Active Threads on " + selectedDate, "No active threads on " + selectedDate + "."
	}
}

func maxTime(a time.Time, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func filterSessionFilesByCwd(files []sessions.SessionFile, cwd string) []sessions.SessionFile {
	if cwd == "" {
		return files
	}
	filtered := make([]sessions.SessionFile, 0, len(files))
	for _, file := range files {
		if sessions.CwdForFile(file) == cwd {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

func (s *Server) buildSessionViews(files []sessions.SessionFile) []sessionView {
	views := make([]sessionView, 0, len(files))
	for _, file := range files {
		views = append(views, s.buildSessionListView(file))
	}
	return views
}

func (s *Server) buildSessionListView(file sessions.SessionFile) sessionView {
	resumeCommand := buildResumeCommand(file.Meta)
	cwd := sessions.CwdForFile(file)
	if cwd == sessions.UnknownCwd {
		cwd = ""
	}
	view := sessionView{
		Name:          file.Name,
		DisplayName:   file.DisplayName(),
		Size:          formatBytes(file.Size),
		ModTime:       formatTime(file.ModTime),
		ResumeCommand: resumeCommand,
		Cwd:           cwd,
		Branch:        branchForMeta(file.Meta),
		BranchURL:     s.branchURLForMeta(file.Meta, cwd),
		DateLabel:     file.Date.String(),
		DatePath:      file.Date.Path(),
	}
	threadStateKey, threadStatusLabel, threadStatusClass, threadAction, threadActionLabel, hasThreadState := s.sessionThreadState(file)
	if hasThreadState {
		view.ThreadStateKey = threadStateKey
		view.ThreadStatusLabel = threadStatusLabel
		view.ThreadStatusClass = threadStatusClass
		view.ThreadAction = threadAction
		view.ThreadActionLabel = threadActionLabel
	}
	return view
}

func (s *Server) buildSessionViewsWithSnippets(files []sessions.SessionFile) []sessionView {
	views := s.buildSessionViews(files)
	for i, file := range files {
		userSnippet, assistantSnippet, hasUser := extractLastSnippets(file)
		if hasUser && userSnippet.Text == "" {
			userSnippet.Text = "(empty)"
		}
		views[i].LastUserSnippet = userSnippet.Text
		views[i].LastUserSnippetTitle = userSnippet.Title
		views[i].LastUserSnippetClass = userSnippet.SpeakerClass
		views[i].LastAssistantSnippet = assistantSnippet.Text
		views[i].LastAssistantSnippetTitle = assistantSnippet.Title
		views[i].LastAssistantSnippetClass = assistantSnippet.SpeakerClass
	}
	return views
}

func (s *Server) buildSessionViewsPage(files []sessions.SessionFile, page int, perPage int, includeAll bool) ([]sessionView, paginationInfo) {
	if includeAll {
		pageFiles, pager := paginateSessionFiles(files, page, perPage)
		return s.buildSessionViewsWithSnippets(pageFiles), pager
	}
	return s.buildSessionViewsPageFiltered(files, page, perPage)
}

func (s *Server) buildSessionViewsPageFiltered(files []sessions.SessionFile, page int, perPage int) ([]sessionView, paginationInfo) {
	if page < 1 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 10
	}
	start := (page - 1) * perPage
	end := start + perPage
	total := 0
	foundNext := false
	scannedAll := true
	views := make([]sessionView, 0, perPage)
	for _, file := range files {
		userSnippet, assistantSnippet, hasUser := extractLastSnippets(file)
		if !hasUser {
			continue
		}
		if total >= start && total < end {
			view := s.buildSessionListView(file)
			if userSnippet.Text == "" {
				userSnippet.Text = "(empty)"
			}
			view.LastUserSnippet = userSnippet.Text
			view.LastUserSnippetTitle = userSnippet.Title
			view.LastUserSnippetClass = userSnippet.SpeakerClass
			view.LastAssistantSnippet = assistantSnippet.Text
			view.LastAssistantSnippetTitle = assistantSnippet.Title
			view.LastAssistantSnippetClass = assistantSnippet.SpeakerClass
			views = append(views, view)
		}
		if total >= end {
			foundNext = true
			scannedAll = false
			break
		}
		total++
	}
	totalPages := 0
	if scannedAll && total > 0 {
		totalPages = (total + perPage - 1) / perPage
		if page > totalPages {
			return s.buildSessionViewsPageFiltered(files, totalPages, perPage)
		}
	}
	info := paginationInfo{
		Page:       page,
		TotalPages: totalPages,
		HasPrev:    page > 1 && total > start,
		HasNext:    foundNext || (totalPages > 0 && page < totalPages),
		PrevPage:   1,
		NextPage:   totalPages,
	}
	if info.HasPrev {
		info.PrevPage = page - 1
	}
	if info.HasNext {
		info.NextPage = page + 1
	} else if totalPages > 0 {
		info.NextPage = totalPages
	}
	return views, info
}

type sessionSnippet struct {
	Text         string
	Title        string
	SpeakerClass string
}

func extractLastSnippets(file sessions.SessionFile) (sessionSnippet, sessionSnippet, bool) {
	session, err := sessions.ParseSession(file.Path)
	if err != nil {
		return sessionSnippet{}, sessionSnippet{}, false
	}
	userSnippet := sessionSnippet{
		Title:        "User",
		SpeakerClass: "user",
	}
	assistantSnippet := sessionSnippet{
		Title:        "Agent",
		SpeakerClass: "agent",
	}
	if session.Meta != nil && session.Meta.IsSubagentThread() {
		userSnippet.Title = "Agent"
		userSnippet.SpeakerClass = "agent"
		assistantSnippet.Title = "Subagent"
		assistantSnippet.SpeakerClass = "subagent"
	}
	hasUser := false
	for _, item := range session.Items {
		switch item.Role {
		case "user":
			if sessions.IsAutoContextUserMessage(item.Content) {
				continue
			}
			hasUser = true
			userSnippet.Text = item.Content
		case "assistant":
			assistantSnippet.Text = item.Content
		}
	}
	userSnippet.Text = snippetFromContent(userSnippet.Text, 180)
	assistantSnippet.Text = snippetFromContent(assistantSnippet.Text, 180)
	return userSnippet, assistantSnippet, hasUser
}

func snippetFromContent(value string, max int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.Join(strings.Fields(value), " ")
	if max <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max > 3 {
		return string(runes[:max-3]) + "..."
	}
	return string(runes[:max])
}

func buildDirViewsFromFiles(files []sessions.SessionFile) []dirView {
	counts := make(map[string]int, len(files))
	for _, file := range files {
		cwd := sessions.CwdForFile(file)
		counts[cwd]++
	}
	return buildDirViewsFromCounts(counts, nil, 0, false)
}

func buildDirViewsFromCounts(counts map[string]int, recentCounts map[string]int, recentMax int, withHeat bool) []dirView {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == sessions.UnknownCwd {
			return false
		}
		if keys[j] == sessions.UnknownCwd {
			return true
		}
		return keys[i] < keys[j]
	})

	views := make([]dirView, 0, len(keys))
	for _, key := range keys {
		view := dirView{
			Label: dirLabel(key),
			Value: key,
			Count: counts[key],
		}
		if withHeat {
			view.RecentCount = recentCounts[key]
			view.HeatColor = heatColor(view.RecentCount, recentMax)
		}
		views = append(views, view)
	}
	return views
}

func previousDateKey(date sessions.DateKey) (sessions.DateKey, bool) {
	year, err := strconv.Atoi(date.Year)
	if err != nil {
		return sessions.DateKey{}, false
	}
	month, err := strconv.Atoi(date.Month)
	if err != nil {
		return sessions.DateKey{}, false
	}
	day, err := strconv.Atoi(date.Day)
	if err != nil {
		return sessions.DateKey{}, false
	}
	current := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
	if current.Year() != year || int(current.Month()) != month || current.Day() != day {
		return sessions.DateKey{}, false
	}
	prev := current.AddDate(0, 0, -1)
	return sessions.DateKey{
		Year:  fmt.Sprintf("%04d", prev.Year()),
		Month: fmt.Sprintf("%02d", int(prev.Month())),
		Day:   fmt.Sprintf("%02d", prev.Day()),
	}, true
}

func dirLabel(cwd string) string {
	if sessions.NormalizeCwd(cwd) == sessions.UnknownCwd {
		return "Unknown (no CWD)"
	}
	return cwd
}

func displayCwd(cwd string) string {
	if sessions.NormalizeCwd(cwd) == sessions.UnknownCwd {
		return ""
	}
	return cwd
}

func (s *Server) recentCwdCounts(since time.Time) (map[string]int, int) {
	counts := map[string]int{}
	max := 0
	for _, date := range s.idx.Dates() {
		files := s.idx.SessionsByDate(date)
		for _, file := range files {
			if file.ModTime.Before(since) {
				continue
			}
			cwd := sessions.CwdForFile(file)
			counts[cwd]++
			if counts[cwd] > max {
				max = counts[cwd]
			}
		}
	}
	return counts, max
}

func (s *Server) recentCwdCountsFromLatestDates(limit int) (map[string]int, int) {
	counts := map[string]int{}
	max := 0
	if limit <= 0 {
		return counts, max
	}
	dates := s.idx.Dates()
	if len(dates) > limit {
		dates = dates[:limit]
	}
	for _, date := range dates {
		files := s.idx.SessionsByDate(date)
		for _, file := range files {
			cwd := sessions.CwdForFile(file)
			counts[cwd]++
			if counts[cwd] > max {
				max = counts[cwd]
			}
		}
	}
	return counts, max
}

func parseHeatMode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "today":
		return "today"
	case "1h", "1hr", "1hour":
		return "1h"
	case "7d", "week", "7days":
		return "7d"
	default:
		return "7d"
	}
}

func heatColor(count int, max int) template.CSS {
	const (
		hotR     = 210
		hotG     = 55
		hotB     = 50
		alphaMin = 0.25
		alphaMax = 0.92
	)
	if max <= 0 || count <= 0 {
		return template.CSS("")
	}
	ratio := float64(count) / float64(max)
	if ratio > 1 {
		ratio = 1
	}
	alpha := alphaMin + (alphaMax-alphaMin)*ratio
	alpha = math.Max(alphaMin, math.Min(alpha, alphaMax))
	return template.CSS(fmt.Sprintf("rgba(%d, %d, %d, %.3f)", hotR, hotG, hotB, alpha))
}

func normalizeCwdParam(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "%") {
		if decoded, err := url.QueryUnescape(value); err == nil {
			value = decoded
		}
	}
	return value
}

func normalizeSearchCwdFilter(value string) string {
	value = normalizeCwdParam(value)
	if value == "" {
		return ""
	}
	if value != "/" && strings.HasSuffix(value, "/") {
		value = strings.TrimRight(value, "/")
	}
	if value != "\\" && strings.HasSuffix(value, "\\") {
		value = strings.TrimRight(value, "\\")
	}
	return value
}

func buildResumeCommand(meta *sessions.SessionMeta) string {
	if meta == nil || meta.ID == "" {
		return ""
	}
	commands := make([]string, 0, 3)
	if meta.Cwd != "" {
		commands = append(commands, "cd "+shellQuote(meta.Cwd))
	}
	if branch := branchForMeta(meta); branch != "" {
		commands = append(commands, "git switch "+shellQuote(branch))
	}
	commands = append(commands, fmt.Sprintf("codex resume %s", meta.ID))
	return strings.Join(commands, "\n")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func branchForMeta(meta *sessions.SessionMeta) string {
	if meta == nil {
		return ""
	}
	return meta.GitBranch()
}

func (s *Server) branchURLForMeta(meta *sessions.SessionMeta, cwd string) string {
	repoURL := normalizeRepositoryURL(s.repositoryURLForMeta(meta, cwd))
	if repoURL == "" {
		return ""
	}
	branch := branchForMeta(meta)
	if branch == "" {
		return repoURL
	}
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return repoURL
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "github.com" && host != "www.github.com" {
		return repoURL
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/tree/" + escapeGitHubPath(branch)
	parsed.RawPath = ""
	return parsed.String()
}

func (s *Server) repositoryURLForMeta(meta *sessions.SessionMeta, cwd string) string {
	if s != nil && s.repoOverrides != nil {
		if overrideURL := s.repoOverrides.ResolveRepositoryURL(cwd); overrideURL != "" {
			return overrideURL
		}
	}
	if meta == nil {
		return ""
	}
	return meta.GitRepositoryURL()
}

func normalizeRepositoryURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(value, "git@github.com:"):
		value = "https://github.com/" + strings.TrimPrefix(value, "git@github.com:")
	case strings.HasPrefix(value, "ssh://git@github.com/"):
		value = "https://github.com/" + strings.TrimPrefix(value, "ssh://git@github.com/")
	}
	value = strings.TrimSuffix(strings.TrimSuffix(value, "/"), ".git")
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return parsed.String()
}

func escapeGitHubPath(value string) string {
	parts := strings.Split(value, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func semanticRoleLabel(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "assistant":
		return "agent"
	case "subagent":
		return "subagent"
	case "user":
		return "user"
	default:
		return strings.ToLower(strings.TrimSpace(role))
	}
}

func sessionSummaryKey(file sessions.SessionFile) string {
	if file.Meta != nil {
		if id := strings.TrimSpace(file.Meta.ID); id != "" {
			return "id:" + id
		}
	}
	return "path:" + filepath.ToSlash(filepath.Join(file.Date.Path(), file.Name))
}

func (s *Server) sessionThreadState(file sessions.SessionFile) (string, string, string, string, string, bool) {
	if s.active == nil || s.activeState == nil {
		return "", "", "", "", "", false
	}
	key := sessionSummaryKey(file)
	summary, ok := s.active.Lookup(key)
	if !ok {
		return "", "", "", "", "", false
	}

	ended := false
	if mark, ok := s.activeState.Snapshot()[summary.Key]; ok {
		ended = mark.ActivityToken == "" || mark.ActivityToken == summary.ActivityToken
	}

	statusLabel, statusClass := activeStatus(summary.WaitState, ended)
	action := "end"
	actionLabel := "⏹️ End"
	if ended {
		action = "reopen"
		actionLabel = "↩️ Reopen"
	}

	return summary.Key, statusLabel, statusClass, action, actionLabel, true
}

func (s *Server) buildSessionView(parts []string) (sessionPageView, error) {
	date, ok := sessions.ParseDate(parts[0], parts[1], parts[2])
	if !ok {
		return sessionPageView{}, errors.New("invalid date")
	}
	filename := parts[3]
	if filename == "" || strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		return sessionPageView{}, errors.New("invalid filename")
	}

	file, ok := s.idx.Lookup(date, filename)
	if !ok {
		return sessionPageView{}, errors.New("file not found")
	}

	session, err := sessions.ParseSession(file.Path)
	if err != nil {
		return sessionPageView{}, err
	}

	toolRunOutputs := findToolRunOutputs(session.Items)
	groupedOutputIndexes := make(map[int]struct{}, len(toolRunOutputs))
	items := make([]itemView, 0, len(session.Items))
	lastUserLine := 0
	lastAnyUserLine := 0
	lastAgentLine := 0
	lastItemLine := 0
	isSubagentThread := session.Meta != nil && session.Meta.IsSubagentThread()
	subagentDisplayName := ""
	subagentDisplayRole := ""
	parentThreadID := ""
	parentSessionPath := ""
	parentSessionTitle := ""
	userNavLabel := "user"
	displayName := file.DisplayName()
	if session.Meta != nil {
		if displayName == file.Name {
			displayName = sessions.SessionDisplayName(file.Name, s.idx.ThreadName(session.Meta.ID))
		}
		subagentDisplayName = session.Meta.SubagentNicknameValue()
		subagentDisplayRole = session.Meta.SubagentRoleValue()
		parentThreadID = session.Meta.ParentThreadID()
		if parentThreadID != "" {
			if parentFile, ok := s.idx.LookupByID(parentThreadID); ok {
				parentSessionPath = "/" + parentFile.Date.Path() + "/" + parentFile.Name + "#page-top"
				parentSessionTitle = formatSessionLinkTitle(parentFile)
			}
		}
		if isSubagentThread {
			userNavLabel = "agent"
		}
	}
	for index := 0; index < len(session.Items); index++ {
		if _, grouped := groupedOutputIndexes[index]; grouped {
			continue
		}

		item := session.Items[index]
		if outputIndex, ok := toolRunOutputs[index]; ok {
			callView := s.buildSessionItemView(item, isSubagentThread, subagentDisplayName, subagentDisplayRole)
			outputItem := session.Items[outputIndex]
			outputView := s.buildSessionItemView(outputItem, isSubagentThread, subagentDisplayName, subagentDisplayRole)
			grouped := buildToolRunView(item, callView, outputItem, outputView)
			groupedOutputIndexes[outputIndex] = struct{}{}
			if outputItem.Line > lastItemLine {
				lastItemLine = outputItem.Line
			}
			items = append(items, grouped)
			continue
		}

		view := s.buildSessionItemView(item, isSubagentThread, subagentDisplayName, subagentDisplayRole)
		autoCtx := view.AutoCtx
		isSubagentNotification := item.SubagentID != ""
		if item.Role == "user" {
			if !isSubagentNotification {
				lastAnyUserLine = item.Line
			}
			if !autoCtx && !isSubagentNotification {
				lastUserLine = item.Line
			}
		}
		if item.Role == "assistant" {
			lastAgentLine = item.Line
		}
		if item.Line > lastItemLine {
			lastItemLine = item.Line
		}
		items = append(items, view)
	}
	if lastUserLine == 0 {
		lastUserLine = lastAnyUserLine
	}

	threadStateKey, threadStatusLabel, threadStatusClass, threadAction, threadActionLabel, hasThreadState := s.sessionThreadState(file)

	view := sessionPageView{
		Date: dateView{
			Label: date.String(),
			Path:  date.Path(),
			Count: 0,
		},
		File: sessionView{
			Name:        file.Name,
			DisplayName: displayName,
			Size:        formatBytes(file.Size),
			ModTime:     formatTime(file.ModTime),
			ModTimeOnly: formatTimeOnly(file.ModTime),
			Cwd:         displayCwd(sessions.CwdForFile(file)),
			Branch:      branchForMeta(file.Meta),
			BranchURL:   s.branchURLForMeta(file.Meta, sessions.CwdForFile(file)),
			DateLabel:   date.String(),
			DatePath:    date.Path(),
		},
		Meta:                session.Meta,
		IsSubagentThread:    isSubagentThread,
		SubagentDisplayName: subagentDisplayName,
		SubagentDisplayRole: subagentDisplayRole,
		ParentThreadID:      parentThreadID,
		ParentSessionPath:   parentSessionPath,
		ParentSessionTitle:  parentSessionTitle,
		UserNavLabel:        userNavLabel,
		Items:               items,
		ResumeCommand:       buildResumeCommand(session.Meta),
		ThreadStateKey:      threadStateKey,
		ThreadStatusLabel:   threadStatusLabel,
		ThreadStatusClass:   threadStatusClass,
		ThreadAction:        threadAction,
		ThreadActionLabel:   threadActionLabel,
		ThemeClass:          s.themeClass,
		IsJSONL:             strings.HasSuffix(strings.ToLower(file.Name), ".jsonl"),
		LastUserLine:        lastUserLine,
		LastAgentLine:       lastAgentLine,
		LastItemLine:        lastItemLine,
	}
	if !hasThreadState {
		view.ThreadStateKey = ""
		view.ThreadStatusLabel = ""
		view.ThreadStatusClass = ""
		view.ThreadAction = ""
		view.ThreadActionLabel = ""
	}
	return view, nil
}

func findToolRunOutputs(items []sessions.RenderItem) map[int]int {
	pendingCalls := make(map[string]int)
	matches := make(map[int]int)
	for index, item := range items {
		switch {
		case isToolRunCall(item):
			pendingCalls[item.CallID] = index
		case isToolRunOutput(item):
			callIndex, ok := pendingCalls[item.CallID]
			if !ok {
				continue
			}
			callItem := items[callIndex]
			if !shouldGroupToolRun(callItem, item) {
				continue
			}
			matches[callIndex] = index
			delete(pendingCalls, item.CallID)
		}
	}
	return matches
}

func isToolRunCall(item sessions.RenderItem) bool {
	if item.Role != "tool" || item.CallID == "" {
		return false
	}
	switch item.Subtype {
	case "function_call", "custom_tool_call":
		return true
	default:
		return false
	}
}

func isToolRunOutput(item sessions.RenderItem) bool {
	if item.Role != "tool" || item.CallID == "" {
		return false
	}
	switch item.Subtype {
	case "function_call_output", "custom_tool_call_output":
		return true
	default:
		return false
	}
}

func (s *Server) buildSessionItemView(item sessions.RenderItem, isSubagentThread bool, subagentDisplayName, subagentDisplayRole string) itemView {
	autoCtx := item.Role == "user" && sessions.IsAutoContextUserMessage(item.Content)
	isSubagentNotification := item.SubagentID != ""
	turnAbortedMessage, isTurnAborted := "", false
	if autoCtx {
		if msg, ok := sessions.ExtractTurnAbortedMessage(item.Content); ok {
			turnAbortedMessage = msg
			isTurnAborted = true
		}
	}
	renderText := item.Content
	if autoCtx && !isTurnAborted {
		renderText = escapeAutoContextTags(renderText)
	}
	view := itemView{
		Line:               item.Line,
		Timestamp:          item.Timestamp,
		Type:               item.Type,
		Subtype:            item.Subtype,
		Role:               item.Role,
		RoleLabel:          semanticRoleLabel(item.Role),
		SpeakerClass:       semanticRoleLabel(item.Role),
		Title:              item.Title,
		Content:            item.Content,
		Class:              item.Class,
		SubagentID:         item.SubagentID,
		SubagentNickname:   item.SubagentNickname,
		SubagentStatusType: item.SubagentStatusType,
		SubagentRequest:    item.SubagentRequest,
		Markdown:           renderItemMarkdown(item),
		HTML:               markdownToHTML(renderText),
	}
	if item.Subtype == "function_call" && item.ToolName == "update_plan" {
		if planHTML := renderUpdatePlanHTML(item.CallID, item.ToolInput); planHTML != "" {
			view.HTML = planHTML
		}
	}
	if item.Subtype == "custom_tool_call" && item.ToolName == "apply_patch" {
		if patchHTML := renderApplyPatchHTML(item.ToolInput); patchHTML != "" {
			metaHTML := markdownToHTML(renderCustomToolCallMetaMarkdown(item))
			view.HTML = template.HTML(string(metaHTML) + string(patchHTML))
		}
	}
	if autoCtx {
		view.AutoCtx = true
		view.Class = strings.TrimSpace(view.Class + " auto-context")
	}
	if isSubagentNotification {
		view.Class = strings.TrimSpace(view.Class + " subagent-notification")
		if item.SubagentRequest != "" {
			view.SubagentRequestHTML = markdownToHTML(item.SubagentRequest)
		}
		if subagentFile, ok := s.idx.LookupByID(item.SubagentID); ok {
			view.SubagentSessionPath = "/" + subagentFile.Date.Path() + "/" + subagentFile.Name + "#page-top"
			view.SubagentSessionTitle = formatSessionLinkTitle(subagentFile)
			if subagentFile.Meta != nil {
				if view.SubagentNickname == "" {
					view.SubagentNickname = subagentFile.Meta.SubagentNicknameValue()
				}
				view.SpeakerRole = subagentFile.Meta.SubagentRoleValue()
			}
		}
		view.SpeakerName = view.SubagentNickname
	}
	if isSubagentThread && !isSubagentNotification {
		switch item.Role {
		case "assistant":
			view.RoleLabel = "subagent"
			view.SpeakerClass = "subagent"
			if item.Subtype == "message" && view.Title == "Agent" {
				view.Title = "Subagent"
			}
			view.SpeakerName = subagentDisplayName
			view.SpeakerRole = subagentDisplayRole
		case "user":
			view.RoleLabel = "agent"
			view.SpeakerClass = "agent"
			if item.Subtype == "message" && view.Title == "User" {
				view.Title = "Agent"
			}
		}
	}
	if view.SpeakerClass != "" {
		view.Class = strings.TrimSpace(view.Class + " speaker-" + view.SpeakerClass)
	}
	if isTurnAborted {
		view.IsTurnAborted = true
		view.TurnAbortedMessage = turnAbortedMessage
	}
	return view
}

func shouldGroupToolRun(callItem, outputItem sessions.RenderItem) bool {
	if callItem.Role != "tool" || outputItem.Role != "tool" {
		return false
	}
	if callItem.CallID == "" || callItem.CallID != outputItem.CallID {
		return false
	}
	switch {
	case callItem.Subtype == "function_call" && outputItem.Subtype == "function_call_output":
		return true
	case callItem.Subtype == "custom_tool_call" && outputItem.Subtype == "custom_tool_call_output":
		return true
	default:
		return false
	}
}

func buildToolRunView(callItem sessions.RenderItem, callView itemView, outputItem sessions.RenderItem, outputView itemView) itemView {
	title := "Tool run"
	subtype := "tool_run"
	if callItem.Subtype == "custom_tool_call" {
		title = "Custom tool run"
		subtype = "custom_tool_run"
	}
	return itemView{
		Line:               callItem.Line,
		Timestamp:          callItem.Timestamp,
		Type:               callItem.Type,
		Subtype:            subtype,
		Role:               callItem.Role,
		RoleLabel:          callView.RoleLabel,
		SpeakerClass:       callView.SpeakerClass,
		Title:              title,
		Content:            strings.TrimSpace(callItem.Content + "\n\n" + outputItem.Content),
		Class:              strings.TrimSpace(callView.Class + " tool-run"),
		Markdown:           renderToolRunMarkdown(title, callItem, outputItem),
		HTML:               callView.HTML,
		ToolRunCallTitle:   callView.Title,
		ToolRunOutputLine:  outputItem.Line,
		ToolRunOutputTitle: outputView.Title,
		ToolRunOutputHTML:  outputView.HTML,
		ToolRunOutputTime:  outputItem.Timestamp,
	}
}

func renderCustomToolCallMetaMarkdown(item sessions.RenderItem) string {
	sections := make([]string, 0, 3)
	if value := strings.TrimSpace(item.ToolName); value != "" {
		sections = append(sections, "**Custom tool:** "+value)
	}
	if value := strings.TrimSpace(item.ToolStatus); value != "" {
		sections = append(sections, "**Status:** "+value)
	}
	if value := strings.TrimSpace(item.CallID); value != "" {
		sections = append(sections, "**Call ID:** "+value)
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func renderApplyPatchHTML(input string) template.HTML {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}

	lines := strings.Split(trimmed, "\n")
	var buf strings.Builder
	buf.WriteString(`<div class="patch-block">`)
	for _, line := range lines {
		buf.WriteString(`<span class="patch-line `)
		buf.WriteString(patchLineClass(line))
		buf.WriteString(`">`)
		buf.WriteString(html.EscapeString(line))
		buf.WriteString(`</span>`)
	}
	buf.WriteString(`</div>`)
	return template.HTML(buf.String())
}

func renderUpdatePlanHTML(callID, input string) template.HTML {
	var payload updatePlanHTMLArgs
	if err := json.Unmarshal([]byte(input), &payload); err != nil {
		return ""
	}

	var buf strings.Builder
	buf.WriteString("<p><strong>Tool:</strong> update_plan</p>")
	if value := strings.TrimSpace(callID); value != "" {
		buf.WriteString("<p><strong>Call ID:</strong> ")
		buf.WriteString(html.EscapeString(value))
		buf.WriteString("</p>")
	}
	if value := strings.TrimSpace(payload.Explanation); value != "" {
		buf.WriteString("<p><strong>Explanation</strong><br>")
		buf.WriteString(renderPlainTextHTML(value))
		buf.WriteString("</p>")
	}

	hasPlan := false
	for _, step := range payload.Plan {
		if strings.TrimSpace(step.Step) != "" {
			hasPlan = true
			break
		}
	}
	if !hasPlan {
		return template.HTML(buf.String())
	}

	buf.WriteString("<p><strong>Plan</strong></p>")
	buf.WriteString(`<ul class="update-plan-list">`)
	for _, step := range payload.Plan {
		text := strings.TrimSpace(step.Step)
		if text == "" {
			continue
		}
		className, marker := updatePlanHTMLStatusParts(step.Status)
		buf.WriteString(`<li class="update-plan-step `)
		buf.WriteString(className)
		buf.WriteString(`">`)
		buf.WriteString(`<span class="update-plan-marker">`)
		buf.WriteString(html.EscapeString(marker))
		buf.WriteString(`</span>`)
		buf.WriteString(`<span class="update-plan-text">`)
		buf.WriteString(renderPlainTextHTML(text))
		buf.WriteString(`</span></li>`)
	}
	buf.WriteString(`</ul>`)
	return template.HTML(buf.String())
}

func renderPlainTextHTML(text string) string {
	escaped := html.EscapeString(strings.TrimSpace(text))
	return strings.ReplaceAll(escaped, "\n", "<br>\n")
}

func updatePlanHTMLStatusParts(status string) (className, marker string) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		return "is-completed", "✅"
	case "in_progress":
		return "is-in-progress", "□"
	case "pending":
		return "is-pending", "□"
	default:
		value := strings.TrimSpace(status)
		if value == "" {
			return "is-pending", "□"
		}
		return "is-unknown", "[" + value + "]"
	}
}

func patchLineClass(line string) string {
	switch {
	case strings.HasPrefix(line, "*** Begin Patch"), strings.HasPrefix(line, "*** End Patch"):
		return "patch-line-marker"
	case strings.HasPrefix(line, "*** Update File:"), strings.HasPrefix(line, "*** Add File:"), strings.HasPrefix(line, "*** Delete File:"), strings.HasPrefix(line, "*** Move to:"):
		return "patch-line-file"
	case strings.HasPrefix(line, "@@"):
		return "patch-line-hunk"
	case strings.HasPrefix(line, "+"):
		return "patch-line-add"
	case strings.HasPrefix(line, "-"):
		return "patch-line-del"
	default:
		return "patch-line-context"
	}
}

func formatSessionLinkTitle(file sessions.SessionFile) string {
	return fmt.Sprintf("%s / %s", file.Date.String(), file.DisplayName())
}

func themeClass(theme int) string {
	switch theme {
	case 1:
		return "theme-noir-blue"
	case 2:
		return "theme-espresso-amber"
	case 3:
		return "theme-graphite-teal"
	case 4:
		return "theme-obsidian-lime"
	case 5:
		return "theme-ink-rose"
	case 6:
		return "theme-iron-cyan"
	default:
		return "theme-graphite-teal"
	}
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func formatUUID(token string) string {
	if len(token) != 32 {
		return token
	}
	return fmt.Sprintf("%s-%s-%s-%s-%s", token[0:8], token[8:12], token[12:16], token[16:20], token[20:32])
}

func (s *Server) buildShareURL(r *http.Request, filename string) string {
	if s.useTailscale && s.tailscaleHost != "" {
		return fmt.Sprintf("https://%s/%s", s.tailscaleHost, filename)
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	host := r.Host
	hostName := host
	if strings.Contains(host, ":") {
		if parsedHost, _, err := net.SplitHostPort(host); err == nil {
			hostName = parsedHost
		}
	}

	if s.shareAddr != "" {
		if strings.HasPrefix(s.shareAddr, ":") {
			host = hostName + s.shareAddr
		} else {
			host = s.shareAddr
		}
	}
	return fmt.Sprintf("%s://%s/%s", scheme, host, filename)
}

func renderItemMarkdown(item sessions.RenderItem) string {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = "Message"
	}
	content := strings.TrimSpace(item.Content)
	if content == "" {
		content = "(empty)"
	}
	if item.SubagentID != "" && strings.TrimSpace(item.SubagentRequest) != "" {
		return fmt.Sprintf("## %s\n\n### Agent request\n\n%s\n\n### Subagent response\n\n%s\n", title, strings.TrimSpace(item.SubagentRequest), content)
	}
	return fmt.Sprintf("## %s\n\n%s\n", title, content)
}

func renderToolRunMarkdown(title string, callItem, outputItem sessions.RenderItem) string {
	sections := []string{
		"## " + strings.TrimSpace(title),
		"### " + strings.TrimSpace(callItem.Title),
		strings.TrimSpace(callItem.Content),
		"### " + strings.TrimSpace(outputItem.Title),
		strings.TrimSpace(outputItem.Content),
	}
	filtered := make([]string, 0, len(sections))
	for _, section := range sections {
		if strings.TrimSpace(section) == "" {
			continue
		}
		filtered = append(filtered, section)
	}
	if len(filtered) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(filtered, "\n\n")) + "\n"
}

func renderSessionMarkdown(items []sessions.RenderItem) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, renderItemMarkdown(item))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n")) + "\n"
}

func joinItemMarkdown(items []itemView) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Markdown) == "" {
			continue
		}
		parts = append(parts, item.Markdown)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n")) + "\n"
}

func sessionItemMarkdown(items []itemView, line int) (string, bool) {
	for _, item := range items {
		if item.Line != line && item.ToolRunOutputLine != line {
			continue
		}
		if strings.TrimSpace(item.Markdown) == "" {
			return "", false
		}
		return item.Markdown, true
	}
	return "", false
}

func escapeAutoContextTags(text string) string {
	replacer := strings.NewReplacer(
		"<INSTRUCTIONS>", "&lt;INSTRUCTIONS&gt;",
		"</INSTRUCTIONS>", "&lt;/INSTRUCTIONS&gt;",
		"<environment_context>", "&lt;environment_context&gt;",
		"</environment_context>", "&lt;/environment_context&gt;",
		"<turn_aborted>", "&lt;turn_aborted&gt;",
		"</turn_aborted>", "&lt;/turn_aborted&gt;",
		"<subagent_notification>", "&lt;subagent_notification&gt;",
		"</subagent_notification>", "&lt;/subagent_notification&gt;",
	)
	return replacer.Replace(text)
}

var markdownEngine = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
)

func markdownToHTML(text string) template.HTML {
	var buf bytes.Buffer
	if err := markdownEngine.Convert([]byte(text), &buf); err != nil {
		return template.HTML(html.EscapeString(text))
	}
	return template.HTML(buf.String())
}
