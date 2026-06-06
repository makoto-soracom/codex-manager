package config

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config captures runtime settings for the server.
type Config struct {
	SessionsDir     string
	KiroSessionsDir string
	Addr            string
	ShareAddr       string
	UseTailscale    bool
	UseHTMLBucket   bool
	NoTrimRequest   bool
	OpenBrowser     bool
	RescanInterval  time.Duration
	ShareDir        string
	Theme           int
}

// Parse reads CLI args into a Config.
func Parse(args []string) (Config, error) {
	fs := flag.NewFlagSet("codex-manager", flag.ContinueOnError)
	var cfg Config
	var showHelp bool
	fs.StringVar(&cfg.SessionsDir, "sessions-dir", "~/.codex/sessions", "Path to codex sessions directory")
	fs.StringVar(&cfg.KiroSessionsDir, "kiro-sessions-dir", "~/.kiro/sessions/cli", "Path to Kiro CLI sessions directory")
	fs.StringVar(&cfg.Addr, "addr", ":8080", "HTTP listen address")
	fs.StringVar(&cfg.ShareAddr, "share-addr", ":8081", "HTTP listen address for share server")
	fs.BoolVar(&cfg.UseTailscale, "ts", false, "Use tailscale serve/funnel for share links")
	fs.BoolVar(&cfg.UseHTMLBucket, "hb", false, "Use htmlbucket share backend")
	fs.BoolVar(&cfg.NoTrimRequest, "full", false, "Do not trim user messages to the request marker")
	fs.BoolVar(&cfg.OpenBrowser, "open-browser", false, "Open the UI in a browser on startup")
	fs.DurationVar(&cfg.RescanInterval, "rescan-interval", 2*time.Minute, "How often to rescan sessions directory")
	fs.StringVar(&cfg.ShareDir, "share-dir", "~/.codex/shares", "Directory to store shared HTML files")
	fs.IntVar(&cfg.Theme, "theme", 3, "Theme palette (1-6): 1=noir-blue, 2=espresso-amber, 3=graphite-teal (default), 4=obsidian-lime, 5=ink-rose, 6=iron-cyan")
	fs.BoolVar(&showHelp, "h", false, "Show help")
	fs.BoolVar(&showHelp, "help", false, "Show help")
	if err := fs.Parse(stripFlagTerminator(args)); err != nil {
		return Config{}, err
	}
	if showHelp {
		fs.Usage()
		return Config{}, flag.ErrHelp
	}

	expanded, err := expandHome(cfg.SessionsDir)
	if err != nil {
		return Config{}, err
	}
	cfg.SessionsDir = expanded

	kiroDir, err := expandHome(cfg.KiroSessionsDir)
	if err != nil {
		return Config{}, err
	}
	cfg.KiroSessionsDir = kiroDir

	shareDir, err := expandHome(cfg.ShareDir)
	if err != nil {
		return Config{}, err
	}
	cfg.ShareDir = shareDir

	if cfg.RescanInterval <= 0 {
		return Config{}, errors.New("rescan-interval must be positive")
	}
	if cfg.Theme < 1 || cfg.Theme > 6 {
		return Config{}, errors.New("theme must be between 1 and 6")
	}

	return cfg, nil
}

func expandHome(path string) (string, error) {
	if path == "" {
		return "", errors.New("sessions-dir cannot be empty")
	}
	if path[0] != '~' {
		return path, nil
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func stripFlagTerminator(args []string) []string {
	if len(args) == 0 {
		return args
	}
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--" {
			continue
		}
		out = append(out, arg)
	}
	return out
}
