package notifications

import (
	"path/filepath"
	"testing"
)

func TestStoreAppendRequestPersistsAndLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notifications.jsonl")

	store, err := LoadStore(path)
	if err != nil {
		t.Fatalf("load store: %v", err)
	}

	entry, err := store.AppendRequest(
		"post",
		"/hook",
		"application/json",
		"curl/8.0",
		"127.0.0.1:12345",
		map[string][]string{"X-Test": {"b", "a"}},
		[]byte("{\"type\":\"task_complete\",\"message\":\"done\"}"),
	)
	if err != nil {
		t.Fatalf("append request: %v", err)
	}
	if entry.ID == "" {
		t.Fatal("expected entry id")
	}
	if !entry.IsJSON {
		t.Fatal("expected json body")
	}
	if entry.PrettyBody == "" {
		t.Fatal("expected pretty json body")
	}
	if entry.Preview == "" {
		t.Fatal("expected preview")
	}

	reloaded, err := LoadStore(path)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	entries := reloaded.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ID != entry.ID {
		t.Fatalf("expected matching id, got %q vs %q", entries[0].ID, entry.ID)
	}
	if entries[0].Headers["X-Test"][0] != "a" {
		t.Fatalf("expected sorted header values, got %#v", entries[0].Headers["X-Test"])
	}
}
