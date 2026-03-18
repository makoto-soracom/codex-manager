package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"codex-manager/internal/active"
	"codex-manager/internal/config"
	"codex-manager/internal/htmlbucket"
	"codex-manager/internal/notifications"
	"codex-manager/internal/render"
	"codex-manager/internal/search"
	"codex-manager/internal/sessions"
	"codex-manager/internal/web"
)

func main() {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatalf("config error: %v", err)
	}
	sessions.SetTrimUserRequestEnabled(!cfg.NoTrimRequest)

	htmlBucketClient, htmlBucketAuthPath, err := setupHTMLBucket(cfg, os.Stdin, os.Stdout)
	if err != nil {
		log.Fatalf("htmlbucket setup error: %v", err)
	}

	idx := sessions.NewIndex(cfg.SessionsDir)
	if err := idx.Refresh(); err != nil {
		log.Printf("initial scan failed: %v", err)
	}

	activeIdx := active.NewIndex()
	if err := activeIdx.RefreshFrom(idx); err != nil {
		log.Printf("initial active index build failed: %v", err)
	}

	activeState, err := active.LoadStateStore(active.DefaultStatePath(cfg.SessionsDir))
	if err != nil {
		log.Printf("active state load failed: %v", err)
		activeState, _ = active.LoadStateStore("")
	}
	if err := activeState.Reconcile(activeIdx.Summaries()); err != nil {
		log.Printf("initial active state reconcile failed: %v", err)
	}

	notificationStore, err := notifications.LoadStore(notifications.DefaultPath(cfg.SessionsDir))
	if err != nil {
		log.Printf("notification store load failed: %v", err)
		notificationStore, _ = notifications.LoadStore("")
	}

	searchIdx := search.NewIndex()
	if err := searchIdx.RefreshFrom(idx); err != nil {
		log.Printf("initial search index build failed: %v", err)
	}

	go func() {
		ticker := time.NewTicker(cfg.RescanInterval)
		defer ticker.Stop()
		for range ticker.C {
			if err := idx.Refresh(); err != nil {
				log.Printf("rescan failed: %v", err)
				continue
			}
			if err := activeIdx.RefreshFrom(idx); err != nil {
				log.Printf("active reindex failed: %v", err)
			}
			if err := activeState.Reconcile(activeIdx.Summaries()); err != nil {
				log.Printf("active state reconcile failed: %v", err)
			}
			if err := searchIdx.RefreshFrom(idx); err != nil {
				log.Printf("search reindex failed: %v", err)
			}
		}
	}()

	renderer, err := render.New()
	if err != nil {
		log.Fatalf("template error: %v", err)
	}

	server := web.NewServer(idx, searchIdx, renderer, cfg.SessionsDir, cfg.ShareDir, cfg.ShareAddr, cfg.Theme)
	server.EnableActive(activeIdx, activeState, 15*time.Second)
	server.EnableNotifications(notificationStore)
	if htmlBucketClient != nil {
		server.EnableHTMLBucket(htmlBucketClient)
		log.Printf("Using htmlbucket share backend (%s)", htmlBucketAuthPath)
	} else {
		log.Printf("Using local share backend (%s)", cfg.ShareDir)
	}
	shareServer := web.NewShareServer(cfg.ShareDir)

	log.Printf("Codex sessions server listening on %s", cfg.Addr)
	log.Printf("Open the UI at %s", urlForAddr(cfg.Addr))
	log.Printf("Share server listening on %s", cfg.ShareAddr)
	log.Printf("Watching sessions in %s", cfg.SessionsDir)
	if cfg.OpenBrowser {
		go func() {
			time.Sleep(250 * time.Millisecond)
			if err := openBrowser(urlForAddr(cfg.Addr)); err != nil {
				log.Printf("failed to open browser: %v", err)
			}
		}()
	}
	go func() {
		if err := http.ListenAndServe(cfg.ShareAddr, shareServer); err != nil {
			log.Fatalf("share server error: %v", err)
		}
	}()
	if cfg.UseTailscale {
		host, err := web.SetupTailscale(cfg.ShareAddr)
		if err != nil {
			log.Fatalf("tailscale setup error: %v", err)
		}
		server.EnableTailscale(host)
		log.Printf("Tailscale share host: %s", host)
	} else {
		log.Printf("Not using tailscale share")
	}
	if err := http.ListenAndServe(cfg.Addr, server); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func setupHTMLBucket(cfg config.Config, stdin io.Reader, stdout io.Writer) (*htmlbucket.Client, string, error) {
	authPath, err := htmlbucket.DefaultAuthPath()
	if err != nil {
		return nil, "", err
	}

	_, err = os.Stat(authPath)
	switch {
	case err == nil:
		auth, err := htmlbucket.LoadAuth(authPath)
		if err != nil {
			return nil, authPath, fmt.Errorf("invalid auth file %s: %w", authPath, err)
		}
		return htmlbucket.NewClient(auth.APIKey), authPath, nil
	case errors.Is(err, os.ErrNotExist):
		if !cfg.UseHTMLBucket {
			return nil, authPath, nil
		}
		apiKey, err := htmlbucket.PromptAPIKey(stdin, stdout)
		if err != nil {
			return nil, authPath, fmt.Errorf("failed to read API key: %w", err)
		}
		if err := htmlbucket.WriteAuth(authPath, apiKey); err != nil {
			return nil, authPath, fmt.Errorf("failed to write auth file %s: %w", authPath, err)
		}
		auth, err := htmlbucket.LoadAuth(authPath)
		if err != nil {
			return nil, authPath, fmt.Errorf("invalid auth file %s: %w", authPath, err)
		}
		return htmlbucket.NewClient(auth.APIKey), authPath, nil
	default:
		return nil, authPath, fmt.Errorf("failed to stat auth file %s: %w", authPath, err)
	}
}

func urlForAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + strings.TrimRight(addr, "/") + "/"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("http://%s:%s/", host, port)
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	if isWSL() {
		cmd = exec.Command("cmd.exe", "/c", "start", "", url)
		return cmd.Start()
	}
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func isWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if data, err := os.ReadFile("/proc/version"); err == nil {
		if strings.Contains(strings.ToLower(string(data)), "microsoft") {
			return true
		}
	}
	if data, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		if strings.Contains(strings.ToLower(string(data)), "microsoft") {
			return true
		}
	}
	return false
}
