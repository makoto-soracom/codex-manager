package search

import (
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"codex-manager/internal/sessions"
)

const (
	defaultLimit  = 50
	maxLimit      = 200
	snippetRadius = 60
	snippetMax    = 180
	contextMax    = 140
)

// Result describes a single search match.
type Result struct {
	Date              string `json:"date"`
	Timestamp         string `json:"timestamp"`
	Cwd               string `json:"cwd"`
	Path              string `json:"path"`
	File              string `json:"file"`
	DisplayFile       string `json:"displayFile"`
	Line              int    `json:"line"`
	Role              string `json:"role"`
	Preview           string `json:"preview"`
	PrevUser          string `json:"prevUser"`
	NextAssistant     string `json:"nextAssistant"`
	PrevUserLine      int    `json:"prevUserLine"`
	NextAssistantLine int    `json:"nextAssistantLine"`

	sortTime time.Time
}

type entry struct {
	date        string
	timestamp   string
	sortTime    time.Time
	cwd         string
	path        string
	file        string
	displayFile string
	line        int
	role        string
	content     string
	lower       string
	prevUser    string
	nextAsst    string
	prevLine    int
	nextLine    int
}

type threadPairKey struct {
	path          string
	file          string
	userLine      int
	assistantLine int
}

type threadPairState struct {
	hasUserHit      bool
	hasAssistantHit bool
}

type fileIndex struct {
	size       int64
	modTime    time.Time
	threadName string
	entries    []entry
}

// Index stores a searchable snapshot of sessions.
type Index struct {
	mu      sync.RWMutex
	files   map[string]fileIndex
	ordered []entry
}

// NewIndex creates an empty search index.
func NewIndex() *Index {
	return &Index{files: map[string]fileIndex{}}
}

// RefreshFrom rebuilds entries for new or changed files in the sessions index.
func (idx *Index) RefreshFrom(sessionsIdx *sessions.Index) error {
	dates := sessionsIdx.Dates()
	files := make([]sessions.SessionFile, 0, len(dates))
	for _, date := range dates {
		files = append(files, sessionsIdx.SessionsByDate(date)...)
	}

	idx.mu.RLock()
	existing := idx.files
	idx.mu.RUnlock()

	next := make(map[string]fileIndex, len(files))
	toParse := make([]sessions.SessionFile, 0)
	for _, file := range files {
		key := file.Path
		if meta, ok := existing[key]; ok && meta.size == file.Size && meta.modTime.Equal(file.ModTime) && meta.threadName == file.ThreadName {
			next[key] = meta
			continue
		}
		toParse = append(toParse, file)
	}

	var firstErr error
	for _, file := range toParse {
		entries, err := buildEntries(file)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if meta, ok := existing[file.Path]; ok {
				next[file.Path] = meta
			}
			continue
		}
		next[file.Path] = fileIndex{size: file.Size, modTime: file.ModTime, threadName: file.ThreadName, entries: entries}
	}

	ordered := make([]entry, 0)
	for _, date := range dates {
		for _, file := range sessionsIdx.SessionsByDate(date) {
			if meta, ok := next[file.Path]; ok {
				ordered = append(ordered, meta.entries...)
			}
		}
	}

	idx.mu.Lock()
	idx.files = next
	idx.ordered = ordered
	idx.mu.Unlock()

	return firstErr
}

// Search returns the first N matches for the query.
func (idx *Index) Search(query string, limit int) []Result {
	return idx.SearchWithCwd(query, limit, "")
}

// SearchWithCwd returns the first N matches for the query filtered by cwd.
// If cwdFilter is empty, it behaves like Search.
func (idx *Index) SearchWithCwd(query string, limit int, cwdFilter string) []Result {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	cwdFilter = normalizeCwdFilter(cwdFilter)
	lower := strings.ToLower(q)

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	matches := make([]entry, 0, limit)
	pairStates := make(map[threadPairKey]threadPairState)
	for _, item := range idx.ordered {
		if !matchesCwdFilter(item.cwd, cwdFilter) {
			continue
		}
		if strings.Index(item.lower, lower) == -1 {
			continue
		}
		matches = append(matches, item)
		if key, ok := pairKeyForEntry(item); ok {
			state := pairStates[key]
			switch item.role {
			case "user":
				state.hasUserHit = true
			case "assistant":
				state.hasAssistantHit = true
			}
			pairStates[key] = state
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].sortTime.After(matches[j].sortTime)
	})

	results := make([]Result, 0, limit)
	for _, item := range matches {
		if shouldSkipAssistantDuplicate(item, pairStates) {
			continue
		}
		preview := makePreview(item.content, q)
		results = append(results, Result{
			Date:              item.date,
			Timestamp:         item.timestamp,
			Cwd:               item.cwd,
			Path:              item.path,
			File:              item.file,
			DisplayFile:       item.displayFile,
			Line:              item.line,
			Role:              item.role,
			Preview:           preview,
			PrevUser:          item.prevUser,
			NextAssistant:     item.nextAsst,
			PrevUserLine:      item.prevLine,
			NextAssistantLine: item.nextLine,
			sortTime:          item.sortTime,
		})
		if len(results) >= limit {
			break
		}
	}

	return results
}

func pairKeyForEntry(item entry) (threadPairKey, bool) {
	switch item.role {
	case "user":
		if item.nextLine <= 0 {
			return threadPairKey{}, false
		}
		return threadPairKey{
			path:          item.path,
			file:          item.file,
			userLine:      item.line,
			assistantLine: item.nextLine,
		}, true
	case "assistant":
		if item.prevLine <= 0 {
			return threadPairKey{}, false
		}
		return threadPairKey{
			path:          item.path,
			file:          item.file,
			userLine:      item.prevLine,
			assistantLine: item.line,
		}, true
	default:
		return threadPairKey{}, false
	}
}

func shouldSkipAssistantDuplicate(item entry, pairStates map[threadPairKey]threadPairState) bool {
	if item.role != "assistant" {
		return false
	}
	key, ok := pairKeyForEntry(item)
	if !ok {
		return false
	}
	state, ok := pairStates[key]
	if !ok {
		return false
	}
	return state.hasUserHit && state.hasAssistantHit
}

func normalizeCwdFilter(value string) string {
	value = strings.TrimSpace(value)
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

func matchesCwdFilter(itemCwd string, cwdFilter string) bool {
	if cwdFilter == "" {
		return true
	}
	if itemCwd == "" {
		return false
	}
	if itemCwd == cwdFilter {
		return true
	}
	if cwdFilter == "/" {
		return strings.HasPrefix(itemCwd, "/")
	}
	if cwdFilter == "\\" {
		return strings.HasPrefix(itemCwd, "\\")
	}
	return strings.HasPrefix(itemCwd, cwdFilter+"/") || strings.HasPrefix(itemCwd, cwdFilter+"\\")
}

func buildEntries(file sessions.SessionFile) ([]entry, error) {
	session, err := sessions.ParseSession(file.Path)
	if err != nil {
		return nil, err
	}

	entries := make([]entry, 0, len(session.Items))
	dateLabel := file.Date.String()
	datePath := file.Date.Path()
	displayFile := file.DisplayName()
	cwd := ""
	if session.Meta != nil && session.Meta.Cwd != "" {
		cwd = session.Meta.Cwd
	} else if file.Meta != nil {
		cwd = file.Meta.Cwd
	}
	cwd = sessions.NormalizeCwd(cwd)

	prevUser := make([]string, len(session.Items))
	prevUserLine := make([]int, len(session.Items))
	lastUser := ""
	lastUserLine := 0
	for i, item := range session.Items {
		prevUser[i] = lastUser
		prevUserLine[i] = lastUserLine
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		if item.Role == "user" && !sessions.IsAutoContextUserMessage(item.Content) {
			lastUser = makeContextSnippet(content)
			lastUserLine = item.Line
		}
	}

	nextAssistant := make([]string, len(session.Items))
	nextAssistantLine := make([]int, len(session.Items))
	nextAsst := ""
	nextAsstLine := 0
	for i := len(session.Items) - 1; i >= 0; i-- {
		nextAssistant[i] = nextAsst
		nextAssistantLine[i] = nextAsstLine
		content := strings.TrimSpace(session.Items[i].Content)
		if content == "" {
			continue
		}
		if session.Items[i].Role == "assistant" {
			nextAsst = makeContextSnippet(content)
			nextAsstLine = session.Items[i].Line
		}
	}

	for i, item := range session.Items {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		timestamp := parseTimestamp(item.Timestamp, file.ModTime)
		entries = append(entries, entry{
			date:        dateLabel,
			timestamp:   formatTimestamp(timestamp),
			sortTime:    timestamp,
			cwd:         cwd,
			path:        datePath,
			file:        file.Name,
			displayFile: displayFile,
			line:        item.Line,
			role:        item.Role,
			content:     content,
			lower:       strings.ToLower(content),
			prevUser:    prevUser[i],
			nextAsst:    nextAssistant[i],
			prevLine:    prevUserLine[i],
			nextLine:    nextAssistantLine[i],
		})
	}
	return entries, nil
}

func makePreview(content string, query string) string {
	cleaned := strings.ReplaceAll(content, "\r", " ")
	cleaned = strings.ReplaceAll(cleaned, "\n", " ")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return ""
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return truncateRunes(cleaned, snippetMax)
	}

	lowerCleaned := strings.ToLower(cleaned)
	lowerQuery := strings.ToLower(query)
	matchIndex := strings.Index(lowerCleaned, lowerQuery)
	if matchIndex == -1 {
		return truncateRunes(cleaned, snippetMax)
	}

	matchRuneIndex := runeOffsetForByteIndex(lowerCleaned, matchIndex)
	queryRuneLen := utf8.RuneCountInString(lowerQuery)
	if queryRuneLen <= 0 {
		return truncateRunes(cleaned, snippetMax)
	}

	runes := []rune(cleaned)
	start := matchRuneIndex - snippetRadius
	if start < 0 {
		start = 0
	}
	end := matchRuneIndex + queryRuneLen + snippetRadius
	if end > len(runes) {
		end = len(runes)
	}
	snippet := strings.TrimSpace(string(runes[start:end]))
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(runes) {
		snippet = snippet + "..."
	}
	return snippet
}

func makeContextSnippet(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.Join(strings.Fields(value), " ")
	return truncateRunes(value, contextMax)
}

func truncateRunes(value string, max int) string {
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

func runeOffsetForByteIndex(value string, byteIndex int) int {
	if byteIndex <= 0 {
		return 0
	}
	if byteIndex >= len(value) {
		return utf8.RuneCountInString(value)
	}
	return utf8.RuneCountInString(value[:byteIndex])
}

func parseTimestamp(value string, fallback time.Time) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts
	}
	if ts, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
		return ts
	}
	return fallback
}

func formatTimestamp(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Format("2006-01-02 15:04:05")
}
