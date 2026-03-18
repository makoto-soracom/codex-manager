package active

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const stateFileVersion = 1

// EndedMark is the persisted manual end marker for a session.
type EndedMark struct {
	ActivityToken string    `json:"activity_token,omitempty"`
	EndedAt       time.Time `json:"ended_at,omitempty"`
}

type persistedState struct {
	Version int                  `json:"version"`
	Ended   map[string]EndedMark `json:"ended,omitempty"`
}

// StateStore persists manual ended/reopened flags independently from session files.
type StateStore struct {
	path string
	mu   sync.RWMutex
	data persistedState
}

// DefaultStatePath returns the default path for persisted session-state metadata.
func DefaultStatePath(sessionsDir string) string {
	return filepath.Join(filepath.Dir(sessionsDir), "session_state.json")
}

// LoadStateStore opens or creates an empty state store.
func LoadStateStore(path string) (*StateStore, error) {
	store := &StateStore{
		path: path,
		data: persistedState{
			Version: stateFileVersion,
			Ended:   map[string]EndedMark{},
		},
	}
	if path == "" {
		return store, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return store, nil
	}

	var decoded persistedState
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	if decoded.Version == 0 {
		decoded.Version = stateFileVersion
	}
	if decoded.Ended == nil {
		decoded.Ended = map[string]EndedMark{}
	}
	store.data = decoded
	return store, nil
}

// Path returns the backing file path.
func (s *StateStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Snapshot returns a copy of the current ended marks.
func (s *StateStore) Snapshot() map[string]EndedMark {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]EndedMark, len(s.data.Ended))
	for key, value := range s.data.Ended {
		out[key] = value
	}
	return out
}

// MarkEnded stores the current activity token as manually ended.
func (s *StateStore) MarkEnded(key string, token string) error {
	if s == nil || key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Version = stateFileVersion
	if s.data.Ended == nil {
		s.data.Ended = map[string]EndedMark{}
	}
	s.data.Ended[key] = EndedMark{
		ActivityToken: token,
		EndedAt:       time.Now().UTC(),
	}
	return s.saveLocked()
}

// Reopen removes an ended mark.
func (s *StateStore) Reopen(key string) error {
	if s == nil || key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.data.Ended) == 0 {
		return nil
	}
	if _, ok := s.data.Ended[key]; !ok {
		return nil
	}
	delete(s.data.Ended, key)
	return s.saveLocked()
}

// Reconcile clears ended markers when the underlying session received new activity.
func (s *StateStore) Reconcile(summaries []Summary) error {
	if s == nil {
		return nil
	}
	current := make(map[string]string, len(summaries))
	for _, summary := range summaries {
		current[summary.Key] = summary.ActivityToken
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.data.Ended) == 0 {
		return nil
	}

	changed := false
	for key, mark := range s.data.Ended {
		token, ok := current[key]
		if !ok || token == "" || mark.ActivityToken == "" {
			continue
		}
		if mark.ActivityToken != token {
			delete(s.data.Ended, key)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.saveLocked()
}

func (s *StateStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}
