package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractRateLimitEventsUsesSessionMetaID(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "rollout-2026-06-01T13-44-57-019e8158-f053-7522-bcc2-8c10c4b18105.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-06-01T04:44:50Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"session-from-meta\",\"timestamp\":\"2026-06-01T04:44:50Z\",\"cwd\":\"/tmp\",\"originator\":\"cli\",\"cli_version\":\"0.1\"}}\n" +
		"{\"timestamp\":\"2026-06-01T04:44:51Z\",\"type\":\"turn_context\",\"payload\":{\"model\":\"gpt-5.5\"}}\n" +
		"{\"timestamp\":\"2026-06-01T04:44:57.945Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"input_tokens\":1,\"total_tokens\":1}},\"rate_limits\":{\"limit_id\":\"codex\",\"primary\":{\"used_percent\":98.0,\"resets_at\":1780292580},\"secondary\":{\"used_percent\":15.0,\"resets_at\":1780879380},\"plan_type\":\"team\"}}}\n" +
		"{\"timestamp\":\"2026-06-01T04:44:58Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"input_tokens\":2,\"total_tokens\":2}},\"rate_limits\":{\"limit_id\":\"codex\",\"primary\":null,\"plan_type\":\"team\"}}}\n"
	if err := os.WriteFile(filePath, []byte(data), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	events, err := ExtractRateLimitEvents(filePath, filepath.Base(filePath))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %#v", len(events), events)
	}
	if events[0].Timestamp != "2026-06-01T04:44:57.945Z" || events[0].Line != 3 || events[0].SessionID != "session-from-meta" || events[0].Model != "gpt-5.5" {
		t.Fatalf("unexpected event: %#v", events[0])
	}
	if events[0].PrimaryUsedPercent == nil || *events[0].PrimaryUsedPercent != 98.0 {
		t.Fatalf("expected primary used_percent 98.0, got %#v", events[0].PrimaryUsedPercent)
	}
	if events[0].PrimaryResetsAt == nil || *events[0].PrimaryResetsAt != 1780292580 {
		t.Fatalf("expected primary resets_at 1780292580, got %#v", events[0].PrimaryResetsAt)
	}
	if events[0].SecondaryUsedPercent == nil || *events[0].SecondaryUsedPercent != 15.0 {
		t.Fatalf("expected secondary used_percent 15.0, got %#v", events[0].SecondaryUsedPercent)
	}
	if events[0].SecondaryResetsAt == nil || *events[0].SecondaryResetsAt != 1780879380 {
		t.Fatalf("expected secondary resets_at 1780879380, got %#v", events[0].SecondaryResetsAt)
	}
}

func TestExtractRateLimitEventsFallsBackToFilenameSessionID(t *testing.T) {
	base := t.TempDir()
	fileName := "rollout-2026-06-01T13-44-57-019e8158-f053-7522-bcc2-8c10c4b18105.jsonl"
	filePath := filepath.Join(base, fileName)
	data := "{\"timestamp\":\"2026-06-01T04:44:57.945Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"rate_limits\":{\"limit_id\":\"codex\",\"secondary\":{\"used_percent\":42.5},\"plan_type\":\"team\"}}}\n"
	if err := os.WriteFile(filePath, []byte(data), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	events, err := ExtractRateLimitEvents(filePath, fileName)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %#v", len(events), events)
	}
	if want := "019e8158-f053-7522-bcc2-8c10c4b18105"; events[0].SessionID != want {
		t.Fatalf("expected fallback session id %q, got %q", want, events[0].SessionID)
	}
	if events[0].PrimaryUsedPercent != nil {
		t.Fatalf("expected nil primary used_percent, got %#v", events[0].PrimaryUsedPercent)
	}
	if events[0].SecondaryUsedPercent == nil || *events[0].SecondaryUsedPercent != 42.5 {
		t.Fatalf("expected secondary used_percent 42.5, got %#v", events[0].SecondaryUsedPercent)
	}
}
