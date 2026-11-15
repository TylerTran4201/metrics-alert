package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func httpClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		slog.Error("metrics-alert stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	const usage = "usage: metrics-alert primary|secondary|check-config [flags]"
	if len(args) == 0 {
		return errors.New(usage)
	}
	mode := args[0]
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	var cfg config
	var listen, healthURL string
	var interval, timeout time.Duration
	var failures int
	switch mode {
	case "primary", "check-config":
		path := flags.String("config", "config/example.yaml", "rule configuration")
		if mode == "primary" {
			flags.StringVar(&listen, "listen", ":8080", "health server address")
		}
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		var err error
		cfg, err = loadConfig(*path)
		if err != nil {
			return fmt.Errorf("primary configuration: %w", err)
		}
	case "secondary":
		flags.StringVar(&healthURL, "url", "http://localhost:8080/healthz", "primary health URL")
		flags.DurationVar(&interval, "interval", 15*time.Second, "health poll interval")
		flags.DurationVar(&timeout, "timeout", 3*time.Second, "health request timeout")
		flags.IntVar(&failures, "failures", 6, "consecutive failed checks before alerting")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if !validURL(healthURL) || timeout <= 0 || interval <= timeout || failures < 1 {
			return errors.New("secondary requires an HTTP(S) url, 0 < timeout < interval, and positive failures")
		}
	default:
		return errors.New(usage)
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if mode == "check-config" {
		fmt.Println("Configuration is valid.")
		return nil
	}
	webhook := os.Getenv("SLACK_WEBHOOK_URL")
	if !validURL(webhook) {
		return errors.New("SLACK_WEBHOOK_URL must contain a valid HTTP(S) URL")
	}

	n := notifier{webhook, httpClient(5 * time.Second), make(chan string, 64)}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go n.run(ctx)
	if mode == "primary" {
		return primary(ctx, cfg, listen, n)
	}
	secondary(ctx, healthURL, interval, timeout, failures, n)
	return nil
}

const scrapeWorkers = 4

func healthHandler(progress *atomic.Pointer[time.Time], maxAge time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		last := progress.Load()
		if last == nil || time.Since(*last) > maxAge {
			http.Error(w, "processing cycle overdue", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func primary(ctx context.Context, cfg config, listen string, n notifier) error {
	var progress atomic.Pointer[time.Time]
	server := &http.Server{Addr: listen, Handler: healthHandler(&progress, cfg.HealthMaxAge), ReadHeaderTimeout: 2 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 30 * time.Second}
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()
	defer server.Close()

	client := httpClient(cfg.Timeout)
	selectors, evaluations := plan(cfg.Rules)
	active := make([]target, 0, len(cfg.Targets))
	for _, target := range cfg.Targets {
		if len(selectors[target.ID]) > 0 {
			active = append(active, target)
		}
	}
	start := 0
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		cycle, cancel := context.WithTimeout(ctx, cfg.Timeout)
		values := scrapeCycle(cycle, client, active, selectors, start)
		start = (start + scrapeWorkers) % len(active)
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		now := time.Now()
		for i := range evaluations {
			e := &evaluations[i]
			message, known := e.evaluate(now, values)
			if !known {
				slog.Warn("rule data unavailable", "rule", e.rule.ID, "scope", e.scope)
			}
			if message != "" {
				n.enqueue(message)
			}
		}
		progress.Store(&now)
		select {
		case <-ctx.Done():
			return nil
		case err := <-serverErr:
			return err
		case <-ticker.C:
		}
	}
	return nil
}

func scrapeCycle(ctx context.Context, client *http.Client, targets []target, selectors map[string][]selector, start int) map[string][]reading {
	values := make(map[string][]reading)
	jobs := make(chan target, len(targets))
	for i := range targets {
		jobs <- targets[(i+start)%len(targets)]
	}
	close(jobs)
	var mu sync.Mutex
	var workers sync.WaitGroup
	for range min(scrapeWorkers, len(targets)) {
		workers.Go(func() {
			for t := range jobs {
				if ctx.Err() != nil {
					slog.Warn("scrape skipped after cycle deadline", "target", t.ID)
					continue
				}
				result, err := scrape(ctx, client, t.URL, selectors[t.ID])
				if err != nil {
					slog.Warn("scrape failed", "target", t.ID, "error", err)
					continue
				}
				mu.Lock()
				values[t.ID] = result
				mu.Unlock()
			}
		})
	}
	workers.Wait()
	return values
}

func secondary(ctx context.Context, endpoint string, interval, timeout time.Duration, failures int, n notifier) {
	client := httpClient(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	missed := 0
	for ctx.Err() == nil {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		resp, err := client.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		if ctx.Err() != nil {
			return
		}
		if err == nil && resp.StatusCode == http.StatusOK {
			if missed == failures {
				n.enqueue("[RESOLVED] Metrics Alert primary is healthy")
			}
			missed = 0
		} else if missed < failures {
			missed++
			if missed == failures {
				n.enqueue("[FIRING] Metrics Alert primary is unavailable")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
