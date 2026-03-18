package sessions

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DateKey identifies a sessions date folder.
type DateKey struct {
	Year  string
	Month string
	Day   string
}

func (d DateKey) String() string {
	return d.Year + "-" + d.Month + "-" + d.Day
}

func (d DateKey) Path() string {
	return path.Join(d.Year, d.Month, d.Day)
}

// SessionFile represents a jsonl file on disk.
type SessionFile struct {
	Date       DateKey
	Name       string
	Path       string
	Size       int64
	ModTime    time.Time
	Meta       *SessionMeta
	ThreadName string
}

// Index stores a snapshot of sessions on disk.
type Index struct {
	baseDir     string
	mu          sync.RWMutex
	byDate      map[DateKey][]SessionFile
	byName      map[string]SessionFile
	byID        map[string]SessionFile
	byCwd       map[string][]SessionFile
	threadNames map[string]string
	updated     time.Time
}

// NewIndex creates an empty index.
func NewIndex(baseDir string) *Index {
	return &Index{
		baseDir:     baseDir,
		byDate:      map[DateKey][]SessionFile{},
		byName:      map[string]SessionFile{},
		byID:        map[string]SessionFile{},
		byCwd:       map[string][]SessionFile{},
		threadNames: map[string]string{},
	}
}

// BaseDir returns the sessions root.
func (idx *Index) BaseDir() string {
	return idx.baseDir
}

// LastUpdated returns when Refresh last succeeded.
func (idx *Index) LastUpdated() time.Time {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.updated
}

// Refresh rescans the sessions directory.
func (idx *Index) Refresh() error {
	if idx.baseDir == "" {
		return errors.New("sessions base directory is empty")
	}
	if _, err := os.Stat(idx.baseDir); err != nil {
		return err
	}

	byDate := map[DateKey][]SessionFile{}
	byName := map[string]SessionFile{}
	byID := map[string]SessionFile{}
	byCwd := map[string][]SessionFile{}
	threadNames := loadThreadNames(sessionIndexPath(idx.baseDir))

	walkErr := filepath.WalkDir(idx.baseDir, func(fullPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}

		rel, err := filepath.Rel(idx.baseDir, fullPath)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 4 {
			return nil
		}
		date, ok := ParseDate(parts[0], parts[1], parts[2])
		if !ok {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		meta, err := ParseSessionMeta(fullPath)
		if err != nil {
			meta = nil
		}

		file := SessionFile{
			Date:    date,
			Name:    parts[3],
			Path:    fullPath,
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Meta:    meta,
		}
		if meta != nil && meta.ID != "" {
			file.ThreadName = threadNames[meta.ID]
		}

		byDate[date] = append(byDate[date], file)
		byName[path.Join(date.Path(), file.Name)] = file
		if file.Meta != nil && file.Meta.ID != "" {
			byID[file.Meta.ID] = file
		}
		cwd := CwdForFile(file)
		byCwd[cwd] = append(byCwd[cwd], file)
		return nil
	})

	if walkErr != nil {
		return walkErr
	}

	for dateKey, files := range byDate {
		sort.Slice(files, func(i, j int) bool {
			if files[i].ModTime.Equal(files[j].ModTime) {
				return files[i].Name < files[j].Name
			}
			return files[i].ModTime.After(files[j].ModTime)
		})
		byDate[dateKey] = files
	}

	idx.mu.Lock()
	idx.byDate = byDate
	idx.byName = byName
	idx.byID = byID
	idx.byCwd = byCwd
	idx.threadNames = threadNames
	idx.updated = time.Now()
	idx.mu.Unlock()
	return nil
}

// Dates returns sorted date keys.
func (idx *Index) Dates() []DateKey {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	keys := make([]DateKey, 0, len(idx.byDate))
	for key := range idx.byDate {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return dateGreater(keys[i], keys[j])
	})
	return keys
}

// SessionsByDate returns sessions for a date.
func (idx *Index) SessionsByDate(date DateKey) []SessionFile {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	files := idx.byDate[date]
	out := make([]SessionFile, len(files))
	copy(out, files)
	return out
}

// Cwds returns sorted working directory keys.
func (idx *Index) Cwds() []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	keys := make([]string, 0, len(idx.byCwd))
	for key := range idx.byCwd {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == UnknownCwd {
			return false
		}
		if keys[j] == UnknownCwd {
			return true
		}
		return keys[i] < keys[j]
	})
	return keys
}

// SessionsByCwd returns sessions for a working directory.
func (idx *Index) SessionsByCwd(cwd string) []SessionFile {
	key := NormalizeCwd(cwd)
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	files := idx.byCwd[key]
	out := make([]SessionFile, len(files))
	copy(out, files)
	return out
}

// CwdCounts returns session counts per working directory.
func (idx *Index) CwdCounts() map[string]int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	counts := make(map[string]int, len(idx.byCwd))
	for key, files := range idx.byCwd {
		counts[key] = len(files)
	}
	return counts
}

// Lookup returns the file for a date+name.
func (idx *Index) Lookup(date DateKey, filename string) (SessionFile, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	file, ok := idx.byName[path.Join(date.Path(), filename)]
	return file, ok
}

// LookupByID returns the file for a session ID.
func (idx *Index) LookupByID(id string) (SessionFile, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	file, ok := idx.byID[id]
	return file, ok
}

// ThreadName returns the latest known thread name for a session ID.
func (idx *Index) ThreadName(id string) string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.threadNames[id]
}

func ParseDate(year, month, day string) (DateKey, bool) {
	if len(year) != 4 || len(month) != 2 || len(day) != 2 {
		return DateKey{}, false
	}
	if !isDigits(year) || !isDigits(month) || !isDigits(day) {
		return DateKey{}, false
	}
	return DateKey{Year: year, Month: month, Day: day}, true
}

func isDigits(value string) bool {
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func dateGreater(a, b DateKey) bool {
	ay, _ := strconv.Atoi(a.Year)
	by, _ := strconv.Atoi(b.Year)
	if ay != by {
		return ay > by
	}
	am, _ := strconv.Atoi(a.Month)
	bm, _ := strconv.Atoi(b.Month)
	if am != bm {
		return am > bm
	}
	ad, _ := strconv.Atoi(a.Day)
	bd, _ := strconv.Atoi(b.Day)
	if ad != bd {
		return ad > bd
	}
	return a.String() > b.String()
}

type sessionIndexEntry struct {
	ID         string `json:"id"`
	ThreadName string `json:"thread_name"`
	UpdatedAt  string `json:"updated_at"`
}

type sessionThreadName struct {
	threadName string
	updatedAt  time.Time
	hasUpdated bool
	line       int
}

func sessionIndexPath(baseDir string) string {
	cleanBaseDir := filepath.Clean(baseDir)
	return filepath.Join(filepath.Dir(cleanBaseDir), "session_index.jsonl")
}

func loadThreadNames(indexPath string) map[string]string {
	file, err := os.Open(indexPath)
	if err != nil {
		return map[string]string{}
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	entries := map[string]sessionThreadName{}
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		lineText := strings.TrimSpace(scanner.Text())
		if lineText == "" {
			continue
		}

		var entry sessionIndexEntry
		if err := json.Unmarshal([]byte(lineText), &entry); err != nil {
			continue
		}

		id := strings.TrimSpace(entry.ID)
		threadName := strings.TrimSpace(entry.ThreadName)
		if id == "" || threadName == "" {
			continue
		}

		updatedAt, hasUpdated := parseSessionIndexUpdatedAt(entry.UpdatedAt)
		current, ok := entries[id]
		if ok && !shouldReplaceThreadName(current, updatedAt, hasUpdated, lineNum) {
			continue
		}
		entries[id] = sessionThreadName{
			threadName: threadName,
			updatedAt:  updatedAt,
			hasUpdated: hasUpdated,
			line:       lineNum,
		}
	}

	out := make(map[string]string, len(entries))
	for id, entry := range entries {
		out[id] = entry.threadName
	}
	return out
}

func parseSessionIndexUpdatedAt(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func shouldReplaceThreadName(current sessionThreadName, updatedAt time.Time, hasUpdated bool, lineNum int) bool {
	if hasUpdated {
		if !current.hasUpdated {
			return true
		}
		if updatedAt.After(current.updatedAt) {
			return true
		}
		if updatedAt.Equal(current.updatedAt) {
			return lineNum > current.line
		}
		return false
	}
	if current.hasUpdated {
		return false
	}
	return lineNum > current.line
}
