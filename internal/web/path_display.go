package web

import (
	"strings"

	"codex-manager/internal/sessions"
)

func displayRenderItem(item sessions.RenderItem, cwd string) sessions.RenderItem {
	if !shortenCwdPathsForRole(item.Role) {
		return item
	}
	item.Content = shortenCwdPathsInText(item.Content, cwd)
	item.SubagentRequest = shortenCwdPathsInText(item.SubagentRequest, cwd)
	return item
}

func shortenCwdPathsForRole(role string) bool {
	switch role {
	case "assistant", "subagent":
		return true
	default:
		return false
	}
}

func shortenCwdPathsInText(text, cwd string) string {
	if text == "" {
		return text
	}
	prefixes := cwdPathPrefixes(cwd)
	if len(prefixes) == 0 {
		return text
	}

	var builder strings.Builder
	offset := 0
	changed := false
	for offset < len(text) {
		matchStart, matchPrefix := nextCwdPathPrefix(text, offset, prefixes)
		if matchStart < 0 {
			break
		}
		if !isCwdPathBoundary(text, matchStart) {
			builder.WriteString(text[offset : matchStart+1])
			offset = matchStart + 1
			continue
		}
		builder.WriteString(text[offset:matchStart])
		offset = matchStart + len(matchPrefix)
		changed = true
	}
	if !changed {
		return text
	}
	builder.WriteString(text[offset:])
	return builder.String()
}

func cwdPathPrefixes(cwd string) []string {
	cwd = strings.TrimSpace(cwd)
	if sessions.NormalizeCwd(cwd) == sessions.UnknownCwd {
		return nil
	}

	bases := []string{cwd}
	if strings.Contains(cwd, `\`) {
		bases = append(bases, strings.ReplaceAll(cwd, `\`, "/"))
	}

	seen := map[string]struct{}{}
	prefixes := make([]string, 0, len(bases)*2)
	for _, base := range bases {
		base = strings.TrimRight(base, `/\`)
		if base == "" {
			continue
		}
		for _, sep := range []string{"/", `\`} {
			prefix := base + sep
			if _, ok := seen[prefix]; ok {
				continue
			}
			seen[prefix] = struct{}{}
			prefixes = append(prefixes, prefix)
		}
	}
	return prefixes
}

func nextCwdPathPrefix(text string, offset int, prefixes []string) (int, string) {
	matchStart := -1
	matchPrefix := ""
	for _, prefix := range prefixes {
		index := strings.Index(text[offset:], prefix)
		if index < 0 {
			continue
		}
		start := offset + index
		if matchStart < 0 || start < matchStart || (start == matchStart && len(prefix) > len(matchPrefix)) {
			matchStart = start
			matchPrefix = prefix
		}
	}
	return matchStart, matchPrefix
}

func isCwdPathBoundary(text string, index int) bool {
	if index <= 0 {
		return true
	}
	switch text[index-1] {
	case '/', '\\':
		return false
	default:
		return true
	}
}
