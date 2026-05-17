package search

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkBuildResultPageFromEntries(b *testing.B) {
	entries := buildBenchmarkEntries(400, 60)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		page := buildPageFromEntries(entries, "needle", 50, 0)
		if len(page.Results) == 0 {
			b.Fatal("expected benchmark search results")
		}
	}
}

func buildBenchmarkEntries(sessionCount, pairCount int) []entry {
	matched := make([]entry, 0, sessionCount*pairCount*2)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sequence := 0

	for session := 0; session < sessionCount; session++ {
		date := fmt.Sprintf("2026-01-%02d", (session%28)+1)
		path := fmt.Sprintf("2026/01/%02d", (session%28)+1)
		file := fmt.Sprintf("session-%04d.jsonl", session)
		cwd := fmt.Sprintf("/repo/%02d", session%10)

		for pair := 0; pair < pairCount; pair++ {
			userLine := pair*2 + 1
			assistantLine := userLine + 1

			userContent := fmt.Sprintf("user question %d %d needle", session, pair)
			assistantContent := fmt.Sprintf("assistant answer %d %d needle", session, pair)

			assistantTime := base.Add(-time.Duration(sequence) * time.Second)
			sequence++
			userTime := base.Add(-time.Duration(sequence) * time.Second)
			sequence++

			userSnippet := makeContextSnippet(userContent)
			assistantSnippet := makeContextSnippet(assistantContent)

			matched = append(matched, entry{
				date:      date,
				timestamp: formatTimestamp(assistantTime),
				sortTime:  assistantTime,
				cwd:       cwd,
				path:      path,
				file:      file,
				line:      assistantLine,
				role:      "assistant",
				content:   assistantContent,
				prevUser:  userSnippet,
				prevLine:  userLine,
			})
			matched = append(matched, entry{
				date:      date,
				timestamp: formatTimestamp(userTime),
				sortTime:  userTime,
				cwd:       cwd,
				path:      path,
				file:      file,
				line:      userLine,
				role:      "user",
				content:   userContent,
				nextAsst:  assistantSnippet,
				nextLine:  assistantLine,
			})
		}
	}

	return matched
}
