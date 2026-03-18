package sessions

import "strings"

// SessionDisplayName returns the user-facing label for a session file.
func SessionDisplayName(filename, threadName string) string {
	threadName = strings.TrimSpace(threadName)
	if threadName == "" {
		return filename
	}
	return threadName + " (" + filename + ")"
}

// DisplayName returns the user-facing label for a session file.
func (f SessionFile) DisplayName() string {
	return SessionDisplayName(f.Name, f.ThreadName)
}
