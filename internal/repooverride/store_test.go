package repooverride

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStoreResolveRepositoryURLPrefersLongestPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session_repository_overrides.json")
	data := `{
  "version": 1,
  "rules": [
    {
      "cwd_prefix": "/home/makoto",
      "repository_url": "https://github.com/example/root.git"
    },
    {
      "cwd_prefix": "/home/makoto/codex-manager",
      "repository_url": "https://github.com/makoto-soracom/codex-manager.git"
    },
    {
      "cwd_prefix": "/home/makoto/codex-manager/docs",
      "repository_url": "https://github.com/makoto-soracom/codex-manager-docs.git"
    }
  ]
}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write overrides: %v", err)
	}

	store, err := LoadStore(path)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	if got := store.ResolveRepositoryURL("/home/makoto/codex-manager"); got != "https://github.com/makoto-soracom/codex-manager.git" {
		t.Fatalf("expected codex-manager override, got %q", got)
	}
	if got := store.ResolveRepositoryURL("/home/makoto/codex-manager/internal/web"); got != "https://github.com/makoto-soracom/codex-manager.git" {
		t.Fatalf("expected codex-manager longest prefix override, got %q", got)
	}
	if got := store.ResolveRepositoryURL("/home/makoto/codex-manager/docs/guides"); got != "https://github.com/makoto-soracom/codex-manager-docs.git" {
		t.Fatalf("expected docs override, got %q", got)
	}
	if got := store.ResolveRepositoryURL("/home/makoto/other"); got != "https://github.com/example/root.git" {
		t.Fatalf("expected parent override, got %q", got)
	}
	if got := store.ResolveRepositoryURL("/home/makoto/codex-manager-extra"); got != "https://github.com/example/root.git" {
		t.Fatalf("expected prefix boundary handling, got %q", got)
	}
}

func TestDefaultPath(t *testing.T) {
	got := DefaultPath("/tmp/codex/sessions")
	want := filepath.Join("/tmp/codex", "session_repository_overrides.json")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
