package sessions

import (
	"os"
	"testing"
)

func TestRefreshKiroReal(t *testing.T) {
	if os.Getenv("CODEX_MANAGER_KIRO_REAL_TEST") != "1" {
		t.Skip("set CODEX_MANAGER_KIRO_REAL_TEST=1 to run against local Kiro session data")
	}
	codexDir := os.Getenv("CODEX_MANAGER_TEST_CODEX_SESSIONS_DIR")
	if codexDir == "" {
		codexDir = "/home/makoto/.codex/sessions"
	}
	kiroDir := os.Getenv("CODEX_MANAGER_TEST_KIRO_SESSIONS_DIR")
	if kiroDir == "" {
		kiroDir = "/home/makoto/.kiro/sessions/cli"
	}

	idx := NewIndex(codexDir, kiroDir)
	if err := idx.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	// Check if any kiro sessions exist
	found := 0
	for _, date := range idx.Dates() {
		for _, f := range idx.SessionsByDate(date) {
			if f.Meta != nil && f.Meta.Originator == "kiro" {
				found++
				if found <= 3 {
					t.Logf("found kiro: %s/%s cwd=%s", date.Path(), f.Name, f.Meta.Cwd)
				}
			}
		}
	}
	t.Logf("total kiro sessions: %d", found)
	if found == 0 {
		t.Fatal("no kiro sessions found in index")
	}
}
