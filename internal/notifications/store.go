package notifications

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry is one received notification request.
type Entry struct {
	ID          string              `json:"id"`
	ReceivedAt  time.Time           `json:"received_at"`
	Method      string              `json:"method"`
	Path        string              `json:"path"`
	ContentType string              `json:"content_type,omitempty"`
	UserAgent   string              `json:"user_agent,omitempty"`
	RemoteAddr  string              `json:"remote_addr,omitempty"`
	Headers     map[string][]string `json:"headers,omitempty"`
	Body        string              `json:"body,omitempty"`
	PrettyBody  string              `json:"pretty_body,omitempty"`
	IsJSON      bool                `json:"is_json,omitempty"`
	Size        int                 `json:"size"`
	Preview     string              `json:"preview,omitempty"`
}

// Store persists received notifications and keeps them in memory for rendering.
type Store struct {
	path    string
	mu      sync.RWMutex
	entries []Entry
}

// DefaultPath returns the default log file for received notifications.
func DefaultPath(sessionsDir string) string {
	return filepath.Join(filepath.Dir(sessionsDir), "notifications.jsonl")
}

// LoadStore opens or creates an empty store.
func LoadStore(path string) (*Store, error) {
	store := &Store{path: path, entries: []Entry{}}
	if path == "" {
		return store, nil
	}

	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, err
		}
		store.entries = append(store.entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return store, nil
}

// AppendRequest records one incoming request body and metadata.
func (s *Store) AppendRequest(method string, path string, contentType string, userAgent string, remoteAddr string, headers map[string][]string, body []byte) (Entry, error) {
	entry, err := buildEntry(method, path, contentType, userAgent, remoteAddr, headers, body)
	if err != nil {
		return Entry{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = append(s.entries, entry)
	if err := s.appendLocked(entry); err != nil {
		s.entries = s.entries[:len(s.entries)-1]
		return Entry{}, err
	}
	return entry, nil
}

// Entries returns notifications ordered from newest to oldest.
func (s *Store) Entries() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.entries) == 0 {
		return nil
	}
	out := make([]Entry, len(s.entries))
	for i := range s.entries {
		out[len(s.entries)-1-i] = s.entries[i]
	}
	return out
}

func buildEntry(method string, path string, contentType string, userAgent string, remoteAddr string, headers map[string][]string, body []byte) (Entry, error) {
	id, err := randomID()
	if err != nil {
		return Entry{}, err
	}

	text := strings.ToValidUTF8(string(body), "\uFFFD")
	text = strings.ReplaceAll(text, "\x00", "")
	pretty := ""
	isJSON := false
	trimmed := strings.TrimSpace(text)
	if trimmed != "" {
		var payload any
		if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
			formatted, err := json.MarshalIndent(payload, "", "  ")
			if err == nil {
				pretty = string(formatted)
				isJSON = true
			}
		}
	}

	return Entry{
		ID:          id,
		ReceivedAt:  time.Now().UTC(),
		Method:      strings.ToUpper(strings.TrimSpace(method)),
		Path:        strings.TrimSpace(path),
		ContentType: strings.TrimSpace(contentType),
		UserAgent:   strings.TrimSpace(userAgent),
		RemoteAddr:  strings.TrimSpace(remoteAddr),
		Headers:     cloneHeaders(headers),
		Body:        text,
		PrettyBody:  pretty,
		IsJSON:      isJSON,
		Size:        len(body),
		Preview:     previewText(text, 160),
	}, nil
}

func cloneHeaders(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		copied := append([]string(nil), values...)
		sort.Strings(copied)
		out[key] = copied
	}
	return out
}

func (s *Store) appendLocked(entry Entry) error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(payload)
	return err
}

func randomID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func previewText(value string, max int) string {
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
