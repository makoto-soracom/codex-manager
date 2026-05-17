package web

import (
	"strings"
	"testing"
)

func TestShortenCwdPathsInText(t *testing.T) {
	cwd := "/home/makoto/work/app"
	input := "" +
		"changed /home/makoto/work/app/src/main.go:12\n" +
		"outside /home/makoto/work/app-other/src/main.go\n" +
		"cwd alone /home/makoto/work/app\n" +
		"url file:///home/makoto/work/app/src/main.go\n"

	got := shortenCwdPathsInText(input, cwd)
	if want := "changed src/main.go:12"; !strings.Contains(got, want) {
		t.Fatalf("expected shortened path %q in %q", want, got)
	}
	if want := "outside /home/makoto/work/app-other/src/main.go"; !strings.Contains(got, want) {
		t.Fatalf("expected sibling path to stay absolute, got %q", got)
	}
	if want := "cwd alone /home/makoto/work/app"; !strings.Contains(got, want) {
		t.Fatalf("expected cwd-only path to stay absolute, got %q", got)
	}
	if want := "url file:///home/makoto/work/app/src/main.go"; !strings.Contains(got, want) {
		t.Fatalf("expected file URL to stay absolute, got %q", got)
	}
}
