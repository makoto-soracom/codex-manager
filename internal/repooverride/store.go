package repooverride

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const fileVersion = 1

type persistedStore struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules,omitempty"`
}

// Rule overrides the repository URL used for sessions under one cwd prefix.
type Rule struct {
	CwdPrefix     string `json:"cwd_prefix"`
	RepositoryURL string `json:"repository_url"`
}

// Store keeps repository URL overrides for specific cwd prefixes.
type Store struct {
	path  string
	rules []Rule
}

// DefaultPath returns the default path for persisted repository overrides.
func DefaultPath(sessionsDir string) string {
	sessionsDir = strings.TrimSpace(sessionsDir)
	if sessionsDir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionsDir), "session_repository_overrides.json")
}

// LoadStore opens or creates an empty override store.
func LoadStore(path string) (*Store, error) {
	store := &Store{path: path}
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
	if strings.TrimSpace(string(data)) == "" {
		return store, nil
	}

	var decoded persistedStore
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	if decoded.Version == 0 {
		decoded.Version = fileVersion
	}
	if decoded.Version != fileVersion {
		return nil, fmt.Errorf("unsupported repository override version %d", decoded.Version)
	}
	store.rules = normalizeRules(decoded.Rules)
	return store, nil
}

// Path returns the backing file path.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// ResolveRepositoryURL returns the best override for cwd using longest-prefix match.
func (s *Store) ResolveRepositoryURL(cwd string) string {
	if s == nil {
		return ""
	}
	cwd = normalizeCwd(cwd)
	if cwd == "" {
		return ""
	}

	bestURL := ""
	bestPrefixLen := -1
	for _, rule := range s.rules {
		if !hasPathPrefix(cwd, rule.CwdPrefix) {
			continue
		}
		if length := len(rule.CwdPrefix); length > bestPrefixLen {
			bestPrefixLen = length
			bestURL = rule.RepositoryURL
		}
	}
	return bestURL
}

func normalizeRules(rules []Rule) []Rule {
	out := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		cwdPrefix := normalizeCwd(rule.CwdPrefix)
		repositoryURL := strings.TrimSpace(rule.RepositoryURL)
		if cwdPrefix == "" || repositoryURL == "" {
			continue
		}
		out = append(out, Rule{
			CwdPrefix:     cwdPrefix,
			RepositoryURL: repositoryURL,
		})
	}
	return out
}

func normalizeCwd(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}

func hasPathPrefix(path string, prefix string) bool {
	if path == prefix {
		return true
	}
	if prefix == string(filepath.Separator) {
		return strings.HasPrefix(path, prefix)
	}
	return strings.HasPrefix(path, prefix+string(filepath.Separator))
}
