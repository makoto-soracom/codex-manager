package active

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"codex-manager/internal/sessions"
)

// WaitState represents what the active thread is waiting on.
type WaitState string

const (
	WaitStateUser  WaitState = "user_waiting"
	WaitStateAgent WaitState = "agent_waiting"
)

// Snippet is a short user/agent preview shown in the active list.
type Snippet struct {
	Text         string
	Title        string
	SpeakerClass string
}

// Summary is the active-thread summary for a single top-level session file.
type Summary struct {
	Key                  string
	SessionID            string
	Date                 sessions.DateKey
	Name                 string
	Path                 string
	DisplayName          string
	ThreadName           string
	Cwd                  string
	Branch               string
	ResumeCommand        string
	Size                 int64
	ModTime              time.Time
	LastActivityAt       time.Time
	ActivityToken        string
	WaitState            WaitState
	LastUserSnippet      Snippet
	LastAssistantSnippet Snippet
	HasUserMessage       bool
}

const thinkingPlaceholder = "Thinking..."

type fileIndex struct {
	size       int64
	modTime    time.Time
	threadName string
	summary    Summary
}

// Index caches active summaries derived from session files.
type Index struct {
	mu      sync.RWMutex
	files   map[string]fileIndex
	byKey   map[string]Summary
	ordered []Summary
	updated time.Time
}

// NewIndex creates an empty active summary index.
func NewIndex() *Index {
	return &Index{
		files: map[string]fileIndex{},
		byKey: map[string]Summary{},
	}
}

// LastUpdated reports when RefreshFrom last succeeded.
func (idx *Index) LastUpdated() time.Time {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.updated
}

// Summaries returns all cached summaries sorted by last activity descending.
func (idx *Index) Summaries() []Summary {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	out := make([]Summary, len(idx.ordered))
	copy(out, idx.ordered)
	return out
}

// Lookup returns a summary by its stable key.
func (idx *Index) Lookup(key string) (Summary, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	summary, ok := idx.byKey[key]
	return summary, ok
}

// RefreshFrom rebuilds changed summaries from the sessions index.
func (idx *Index) RefreshFrom(sessionsIdx *sessions.Index) error {
	dates := sessionsIdx.Dates()
	files := make([]sessions.SessionFile, 0, len(dates))
	for _, date := range dates {
		files = append(files, sessionsIdx.SessionsByDate(date)...)
	}

	idx.mu.RLock()
	existing := idx.files
	idx.mu.RUnlock()

	nextFiles := make(map[string]fileIndex, len(files))
	nextByKey := make(map[string]Summary, len(files))
	summaries := make([]Summary, 0, len(files))
	var firstErr error

	for _, file := range files {
		if file.Meta != nil && file.Meta.IsSubagentThread() {
			continue
		}
		if cached, ok := existing[file.Path]; ok && cached.size == file.Size && cached.modTime.Equal(file.ModTime) && cached.threadName == file.ThreadName {
			nextFiles[file.Path] = cached
			nextByKey[cached.summary.Key] = cached.summary
			summaries = append(summaries, cached.summary)
			continue
		}

		summary, err := buildSummary(file)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if cached, ok := existing[file.Path]; ok {
				nextFiles[file.Path] = cached
				nextByKey[cached.summary.Key] = cached.summary
				summaries = append(summaries, cached.summary)
			}
			continue
		}

		entry := fileIndex{
			size:       file.Size,
			modTime:    file.ModTime,
			threadName: file.ThreadName,
			summary:    summary,
		}
		nextFiles[file.Path] = entry
		nextByKey[summary.Key] = summary
		summaries = append(summaries, summary)
	}

	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].LastActivityAt.Equal(summaries[j].LastActivityAt) {
			if summaries[i].ModTime.Equal(summaries[j].ModTime) {
				if summaries[i].Date.String() == summaries[j].Date.String() {
					return summaries[i].Name < summaries[j].Name
				}
				return summaries[i].Date.String() > summaries[j].Date.String()
			}
			return summaries[i].ModTime.After(summaries[j].ModTime)
		}
		return summaries[i].LastActivityAt.After(summaries[j].LastActivityAt)
	})

	idx.mu.Lock()
	idx.files = nextFiles
	idx.byKey = nextByKey
	idx.ordered = summaries
	idx.updated = time.Now()
	idx.mu.Unlock()
	return firstErr
}

func buildSummary(file sessions.SessionFile) (Summary, error) {
	activity, err := scanActivity(file)
	if err != nil {
		return Summary{}, err
	}

	userSnippet, assistantSnippet, hasUser, assistantAfterLatestUser, err := extractSnippets(file.Path, file.Meta)
	if err != nil {
		userSnippet = Snippet{Title: "User", SpeakerClass: "user"}
		assistantSnippet = Snippet{Title: "Agent", SpeakerClass: "agent"}
		hasUser = false
		assistantAfterLatestUser = false
	}
	if activity.WaitState == WaitStateAgent && hasUser && !assistantAfterLatestUser {
		assistantSnippet.Text = thinkingPlaceholder
	}

	return Summary{
		Key:                  summaryKey(file),
		SessionID:            sessionID(file.Meta),
		Date:                 file.Date,
		Name:                 file.Name,
		Path:                 file.Path,
		DisplayName:          file.DisplayName(),
		ThreadName:           file.ThreadName,
		Cwd:                  sessions.CwdForFile(file),
		Branch:               branchForMeta(file.Meta),
		ResumeCommand:        buildResumeCommand(file.Meta),
		Size:                 file.Size,
		ModTime:              file.ModTime,
		LastActivityAt:       activity.LastActivityAt,
		ActivityToken:        activity.ActivityToken,
		WaitState:            activity.WaitState,
		LastUserSnippet:      userSnippet,
		LastAssistantSnippet: assistantSnippet,
		HasUserMessage:       hasUser,
	}, nil
}

// BuildSummary derives an active-thread summary for one session file.
func BuildSummary(file sessions.SessionFile) (Summary, error) {
	return buildSummary(file)
}

func summaryKey(file sessions.SessionFile) string {
	if file.Meta != nil {
		if id := strings.TrimSpace(file.Meta.ID); id != "" {
			return "id:" + id
		}
	}
	return "path:" + path.Join(file.Date.Path(), file.Name)
}

func sessionID(meta *sessions.SessionMeta) string {
	if meta == nil {
		return ""
	}
	return strings.TrimSpace(meta.ID)
}

func buildResumeCommand(meta *sessions.SessionMeta) string {
	if meta == nil || strings.TrimSpace(meta.ID) == "" {
		return ""
	}
	commands := make([]string, 0, 3)
	if cwd := strings.TrimSpace(meta.Cwd); cwd != "" {
		commands = append(commands, "cd "+shellQuote(cwd))
	}
	if branch := branchForMeta(meta); branch != "" {
		commands = append(commands, "git switch "+shellQuote(branch))
	}
	commands = append(commands, "codex resume "+strings.TrimSpace(meta.ID))
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

type activityInfo struct {
	LastActivityAt time.Time
	ActivityToken  string
	WaitState      WaitState
}

type activityEnvelope struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

func scanActivity(file sessions.SessionFile) (activityInfo, error) {
	f, err := os.Open(file.Path)
	if err != nil {
		return activityInfo{}, err
	}
	defer f.Close()

	reader := bufio.NewReader(f)

	var lastActivity time.Time
	lineCount := 0
	waitState := WaitStateUser

	for {
		rawLine, err := reader.ReadBytes('\n')
		if len(rawLine) > 0 {
			line := strings.TrimSpace(string(rawLine))
			if line != "" {
				lineCount++

				var env activityEnvelope
				if err := json.Unmarshal([]byte(line), &env); err == nil {
					if ts, ok := parseTimestamp(env.Timestamp); ok {
						lastActivity = ts
					}

					if env.Type == "event_msg" {
						var payload struct {
							Type string `json:"type"`
						}
						if err := json.Unmarshal(env.Payload, &payload); err == nil {
							switch payload.Type {
							case "task_started":
								waitState = WaitStateAgent
							case "task_complete":
								waitState = WaitStateUser
							}
						}
					}
				}
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return activityInfo{}, err
		}
	}

	if lastActivity.IsZero() {
		if file.Meta != nil {
			if ts, ok := parseTimestamp(file.Meta.Timestamp); ok {
				lastActivity = ts
			}
		}
		if lastActivity.IsZero() {
			lastActivity = file.ModTime
		}
	}
	if lineCount == 0 {
		lineCount = 1
	}

	return activityInfo{
		LastActivityAt: lastActivity,
		ActivityToken:  fmt.Sprintf("%d:%d:%d", file.Size, lastActivity.UnixNano(), lineCount),
		WaitState:      waitState,
	}, nil
}

func parseTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

func extractSnippets(path string, meta *sessions.SessionMeta) (Snippet, Snippet, bool, bool, error) {
	snippets, err := sessions.ExtractLastConversationSnippets(path)
	if err != nil {
		return Snippet{}, Snippet{}, false, false, err
	}

	userSnippet := Snippet{
		Title:        "User",
		SpeakerClass: "user",
	}
	assistantSnippet := Snippet{
		Title:        "Agent",
		SpeakerClass: "agent",
	}
	if snippets.Meta != nil && snippets.Meta.IsSubagentThread() {
		userSnippet.Title = "Agent"
		userSnippet.SpeakerClass = "agent"
		assistantSnippet.Title = "Subagent"
		assistantSnippet.SpeakerClass = "subagent"
	} else if meta != nil && meta.IsSubagentThread() {
		userSnippet.Title = "Agent"
		userSnippet.SpeakerClass = "agent"
		assistantSnippet.Title = "Subagent"
		assistantSnippet.SpeakerClass = "subagent"
	}

	userSnippet.Text = snippets.LastUser
	assistantSnippet.Text = snippets.LastAssistant
	userSnippet.Text = snippetFromContent(userSnippet.Text, 180)
	assistantSnippet.Text = snippetFromContent(assistantSnippet.Text, 180)
	return userSnippet, assistantSnippet, snippets.HasUser, snippets.AssistantAfterLastUser, nil
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
