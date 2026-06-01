package sessions

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
)

var sessionIDInFilenamePattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// RateLimitEvent is a token_count event with rate-limit usage.
type RateLimitEvent struct {
	Timestamp            string
	Line                 int
	SessionID            string
	Model                string
	PrimaryUsedPercent   *float64
	PrimaryResetsAt      *int64
	SecondaryUsedPercent *float64
	SecondaryResetsAt    *int64
}

type rateLimitEnvelope struct {
	Timestamp string           `json:"timestamp"`
	Type      string           `json:"type"`
	Payload   rateLimitPayload `json:"payload"`
}

type rateLimitPayload struct {
	ID         string             `json:"id"`
	Model      string             `json:"model"`
	Type       string             `json:"type"`
	RateLimits *rateLimitSnapshot `json:"rate_limits"`
}

type rateLimitSnapshot struct {
	Primary   *rateLimitWindow `json:"primary"`
	Secondary *rateLimitWindow `json:"secondary"`
}

type rateLimitWindow struct {
	UsedPercent *float64 `json:"used_percent"`
	ResetsAt    *int64   `json:"resets_at"`
}

// ExtractRateLimitEvents reads token_count rate-limit rows from a session JSONL file.
func ExtractRateLimitEvents(path string, fallbackName string) ([]RateLimitEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	sessionID := ""
	model := ""
	events := []RateLimitEvent{}
	reader := bufio.NewReader(file)
	lineNum := 0
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineNum++
			lineText := strings.TrimRight(string(line), "\r\n")
			var env rateLimitEnvelope
			if json.Unmarshal([]byte(lineText), &env) == nil {
				switch env.Type {
				case "session_meta":
					if id := strings.TrimSpace(env.Payload.ID); id != "" {
						sessionID = id
					}
				case "turn_context":
					if value := strings.TrimSpace(env.Payload.Model); value != "" {
						model = value
					}
				case "event_msg":
					if env.Payload.Type == "token_count" && hasUsedPercent(env.Payload.RateLimits) {
						events = append(events, RateLimitEvent{
							Timestamp:            env.Timestamp,
							Line:                 lineNum,
							SessionID:            sessionID,
							Model:                model,
							PrimaryUsedPercent:   usedPercent(env.Payload.RateLimits.Primary),
							PrimaryResetsAt:      resetsAt(env.Payload.RateLimits.Primary),
							SecondaryUsedPercent: usedPercent(env.Payload.RateLimits.Secondary),
							SecondaryResetsAt:    resetsAt(env.Payload.RateLimits.Secondary),
						})
					}
				}
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	fallbackID := strings.TrimSpace(sessionID)
	if fallbackID == "" {
		fallbackID = sessionIDFromFilename(fallbackName)
	}
	for index := range events {
		if strings.TrimSpace(events[index].SessionID) == "" {
			events[index].SessionID = fallbackID
		}
	}
	return events, nil
}

func sessionIDFromFilename(name string) string {
	return sessionIDInFilenamePattern.FindString(name)
}

func hasUsedPercent(limits *rateLimitSnapshot) bool {
	if limits == nil {
		return false
	}
	return (limits.Primary != nil && limits.Primary.UsedPercent != nil) ||
		(limits.Secondary != nil && limits.Secondary.UsedPercent != nil)
}

func usedPercent(window *rateLimitWindow) *float64 {
	if window == nil {
		return nil
	}
	return window.UsedPercent
}

func resetsAt(window *rateLimitWindow) *int64 {
	if window == nil {
		return nil
	}
	return window.ResetsAt
}
