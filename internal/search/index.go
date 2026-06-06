package search

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"codex-manager/internal/sessions"
)

const (
	defaultLimit    = 50
	maxLimit        = 200
	snippetRadius   = 60
	snippetMax      = 180
	contextMax      = 140
	searchBatchSize = 256
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

// Page contains one search result page and its total size.
type Page struct {
	Results []Result
	Offset  int
	Limit   int
	Total   int
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

type lineMatcher struct {
	lines []int
}

// Index stores a lightweight searchable snapshot of session files.
type Index struct {
	mu       sync.RWMutex
	files    map[string]sessions.SessionFile
	rgPath   string
	grepPath string
}

// NewIndex creates an empty search index.
func NewIndex() *Index {
	return &Index{
		files:    map[string]sessions.SessionFile{},
		rgPath:   lookupSearchBinary("rg"),
		grepPath: lookupSearchBinary("grep"),
	}
}

// RefreshFrom snapshots session file metadata for later query-time searching.
func (idx *Index) RefreshFrom(sessionsIdx *sessions.Index) error {
	dates := sessionsIdx.Dates()
	files := make(map[string]sessions.SessionFile)
	for _, date := range dates {
		for _, file := range sessionsIdx.SessionsByDate(date) {
			files[file.Path] = file
		}
	}

	idx.mu.Lock()
	idx.files = files
	idx.mu.Unlock()
	return nil
}

// Search returns the first N matches for the query.
func (idx *Index) Search(query string, limit int) []Result {
	page, err := idx.SearchPageWithCwdContext(context.Background(), query, limit, 0, "")
	if err != nil {
		return nil
	}
	return page.Results
}

// SearchWithCwd returns the first N matches for the query filtered by cwd.
func (idx *Index) SearchWithCwd(query string, limit int, cwdFilter string) []Result {
	page, err := idx.SearchPageWithCwdContext(context.Background(), query, limit, 0, cwdFilter)
	if err != nil {
		return nil
	}
	return page.Results
}

// SearchPageWithCwdContext returns one result page filtered by cwd.
func (idx *Index) SearchPageWithCwdContext(ctx context.Context, query string, limit int, offset int, cwdFilter string) (Page, error) {
	q := strings.TrimSpace(query)
	limit = normalizeLimit(limit)
	if offset < 0 {
		offset = 0
	}
	cwdFilter = normalizeCwdFilter(cwdFilter)
	if q == "" {
		return Page{Offset: offset, Limit: limit}, nil
	}

	files, rgPath, grepPath := idx.snapshotFiles(cwdFilter)
	if len(files) == 0 {
		return Page{Offset: offset, Limit: limit}, nil
	}

	rawHits, err := collectRawHits(ctx, q, files, rgPath, grepPath)
	if err != nil {
		return Page{}, err
	}

	matched := make([]entry, 0, len(rawHits))
	for _, file := range files {
		lines := rawHits[file.Path]
		if len(lines) == 0 {
			continue
		}
		entries, err := buildEntries(file, newLineMatcher(lines))
		if err != nil {
			continue
		}
		matched = append(matched, entries...)
	}

	return buildPageFromEntries(matched, q, limit, offset), nil
}

func (idx *Index) snapshotFiles(cwdFilter string) ([]sessions.SessionFile, string, string) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	files := make([]sessions.SessionFile, 0, len(idx.files))
	for _, file := range idx.files {
		if !matchesCwdFilter(sessions.CwdForFile(file), cwdFilter) {
			continue
		}
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Date.String() == files[j].Date.String() {
			return files[i].Name < files[j].Name
		}
		return files[i].Date.String() > files[j].Date.String()
	})
	return files, idx.rgPath, idx.grepPath
}

func collectRawHits(ctx context.Context, query string, files []sessions.SessionFile, rgPath string, grepPath string) (map[string][]int, error) {
	if rgPath != "" {
		hits, err := runRipgrep(ctx, rgPath, query, files)
		if err == nil {
			return hits, nil
		}
		if ctx.Err() != nil {
			return nil, err
		}
	}
	if grepPath != "" {
		hits, err := runGrep(ctx, grepPath, query, files)
		if err == nil {
			return hits, nil
		}
		if ctx.Err() != nil {
			return nil, err
		}
	}
	return scanFiles(ctx, query, files)
}

func runRipgrep(ctx context.Context, rgPath string, query string, files []sessions.SessionFile) (map[string][]int, error) {
	rawHits := map[string]map[int]struct{}{}
	for _, batch := range chunkSessionFiles(files) {
		if err := runRipgrepBatch(ctx, rgPath, query, batch, rawHits); err != nil {
			return nil, err
		}
	}
	return compactRawHits(rawHits), nil
}

func runRipgrepBatch(ctx context.Context, rgPath string, query string, files []sessions.SessionFile, rawHits map[string]map[int]struct{}) error {
	args := []string{"--json", "-F", "-i", "-n", "--no-messages", "-e", query}
	for _, file := range files {
		args = append(args, file.Path)
	}

	cmd := exec.CommandContext(ctx, rgPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return err
	}

	parseErr := parseRipgrepMatches(stdout, rawHits)
	waitErr := cmd.Wait()
	if parseErr != nil {
		return parseErr
	}
	if isNoMatchExit(waitErr) {
		return nil
	}
	return waitErr
}

func parseRipgrepMatches(stdout io.Reader, rawHits map[string]map[int]struct{}) error {
	type rgMessage struct {
		Type string `json:"type"`
		Data struct {
			Path struct {
				Text string `json:"text"`
			} `json:"path"`
			LineNumber int `json:"line_number"`
		} `json:"data"`
	}

	reader := bufio.NewReader(stdout)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			text := strings.TrimSpace(string(line))
			if text != "" {
				var message rgMessage
				if unmarshalErr := json.Unmarshal([]byte(text), &message); unmarshalErr != nil {
					return unmarshalErr
				}
				if message.Type == "match" && message.Data.LineNumber > 0 {
					addRawHit(rawHits, filepath.Clean(message.Data.Path.Text), message.Data.LineNumber)
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func runGrep(ctx context.Context, grepPath string, query string, files []sessions.SessionFile) (map[string][]int, error) {
	rawHits := map[string]map[int]struct{}{}
	for _, batch := range chunkSessionFiles(files) {
		if err := runGrepBatch(ctx, grepPath, query, batch, rawHits); err != nil {
			return nil, err
		}
	}
	return compactRawHits(rawHits), nil
}

func runGrepBatch(ctx context.Context, grepPath string, query string, files []sessions.SessionFile, rawHits map[string]map[int]struct{}) error {
	args := []string{"-H", "-I", "-F", "-i", "-n", "--", query}
	for _, file := range files {
		args = append(args, file.Path)
	}

	cmd := exec.CommandContext(ctx, grepPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return err
	}

	parseErr := parseGrepMatches(stdout, rawHits)
	waitErr := cmd.Wait()
	if parseErr != nil {
		return parseErr
	}
	if isNoMatchExit(waitErr) {
		return nil
	}
	return waitErr
}

func parseGrepMatches(stdout io.Reader, rawHits map[string]map[int]struct{}) error {
	reader := bufio.NewReader(stdout)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			path, lineNumber, ok := parseGrepLine(strings.TrimRight(string(line), "\r\n"))
			if ok {
				addRawHit(rawHits, filepath.Clean(path), lineNumber)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func parseGrepLine(line string) (string, int, bool) {
	firstColon := strings.IndexByte(line, ':')
	if firstColon <= 0 {
		return "", 0, false
	}
	secondColon := strings.IndexByte(line[firstColon+1:], ':')
	if secondColon <= 0 {
		return "", 0, false
	}
	secondColon += firstColon + 1
	lineNumber, err := strconv.Atoi(line[firstColon+1 : secondColon])
	if err != nil || lineNumber <= 0 {
		return "", 0, false
	}
	return line[:firstColon], lineNumber, true
}

func scanFiles(ctx context.Context, query string, files []sessions.SessionFile) (map[string][]int, error) {
	rawHits := map[string]map[int]struct{}{}
	lowerQuery := strings.ToLower(query)
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f, err := os.Open(file.Path)
		if err != nil {
			continue
		}
		reader := bufio.NewReader(f)
		lineNumber := 0
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				lineNumber++
				text := strings.TrimRight(string(line), "\r\n")
				if strings.Contains(strings.ToLower(text), lowerQuery) {
					addRawHit(rawHits, file.Path, lineNumber)
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				_ = f.Close()
				return nil, err
			}
		}
		_ = f.Close()
	}
	return compactRawHits(rawHits), nil
}

func chunkSessionFiles(files []sessions.SessionFile) [][]sessions.SessionFile {
	if len(files) == 0 {
		return nil
	}
	batches := make([][]sessions.SessionFile, 0, (len(files)+searchBatchSize-1)/searchBatchSize)
	for start := 0; start < len(files); start += searchBatchSize {
		end := start + searchBatchSize
		if end > len(files) {
			end = len(files)
		}
		batches = append(batches, files[start:end])
	}
	return batches
}

func addRawHit(rawHits map[string]map[int]struct{}, path string, line int) {
	if path == "" || line <= 0 {
		return
	}
	lines := rawHits[path]
	if lines == nil {
		lines = map[int]struct{}{}
		rawHits[path] = lines
	}
	lines[line] = struct{}{}
}

func compactRawHits(rawHits map[string]map[int]struct{}) map[string][]int {
	out := make(map[string][]int, len(rawHits))
	for path, lines := range rawHits {
		values := make([]int, 0, len(lines))
		for line := range lines {
			values = append(values, line)
		}
		sort.Ints(values)
		out[path] = values
	}
	return out
}

func newLineMatcher(lines []int) lineMatcher {
	return lineMatcher{lines: lines}
}

func (m lineMatcher) Matches(start, end int) bool {
	if len(m.lines) == 0 {
		return false
	}
	if start <= 0 {
		start = 1
	}
	if end < start {
		end = start
	}
	index := sort.SearchInts(m.lines, start)
	return index < len(m.lines) && m.lines[index] <= end
}

func buildEntries(file sessions.SessionFile, matcher lineMatcher) ([]entry, error) {
	session, err := sessions.ParseSessionForFile(file)
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
		endLine := item.EndLine
		if endLine <= 0 {
			endLine = item.Line
		}
		if !matcher.Matches(item.Line, endLine) {
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
			prevUser:    prevUser[i],
			nextAsst:    nextAssistant[i],
			prevLine:    prevUserLine[i],
			nextLine:    nextAssistantLine[i],
		})
	}
	return entries, nil
}

func buildPageFromEntries(matched []entry, query string, limit int, offset int) Page {
	matched = filterEntriesByQuery(matched, query)
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].sortTime.Equal(matched[j].sortTime) {
			if matched[i].path == matched[j].path {
				if matched[i].file == matched[j].file {
					return matched[i].line < matched[j].line
				}
				return matched[i].file < matched[j].file
			}
			return matched[i].path < matched[j].path
		}
		return matched[i].sortTime.After(matched[j].sortTime)
	})

	pairStates := make(map[threadPairKey]threadPairState, len(matched))
	for _, item := range matched {
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

	page := Page{
		Results: make([]Result, 0, limit),
		Offset:  offset,
		Limit:   limit,
	}
	for _, item := range matched {
		if shouldSkipAssistantDuplicate(item, pairStates) {
			continue
		}
		if page.Total >= offset && len(page.Results) < limit {
			page.Results = append(page.Results, Result{
				Date:              item.date,
				Timestamp:         item.timestamp,
				Cwd:               item.cwd,
				Path:              item.path,
				File:              item.file,
				DisplayFile:       item.displayFile,
				Line:              item.line,
				Role:              item.role,
				Preview:           makePreview(item.content, query),
				PrevUser:          item.prevUser,
				NextAssistant:     item.nextAsst,
				PrevUserLine:      item.prevLine,
				NextAssistantLine: item.nextLine,
				sortTime:          item.sortTime,
			})
		}
		page.Total++
	}
	return page
}

func filterEntriesByQuery(entries []entry, query string) []entry {
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	if lowerQuery == "" {
		return entries
	}
	filtered := entries[:0]
	for _, item := range entries {
		if strings.Contains(strings.ToLower(item.content), lowerQuery) {
			filtered = append(filtered, item)
		}
	}
	return filtered
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

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
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

func lookupSearchBinary(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

func isNoMatchExit(err error) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return exitErr.ExitCode() == 1
}
