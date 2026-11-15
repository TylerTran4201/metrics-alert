package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfig(t *testing.T) {
	cfg, err := loadConfig("../../config/example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 3 || cfg.Rules[1].For != 3*time.Second {
		t.Fatalf("unexpected example rules: %+v", cfg.Rules)
	}
	path := filepath.Join(t.TempDir(), "rules.yaml")
	if err := os.WriteFile(path, []byte("unexpected_key: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil || !strings.Contains(err.Error(), "unexpected_key") {
		t.Fatalf("want invalid field error, got %v", err)
	}
}

func TestScrape(t *testing.T) {
	selectors := []selector{
		{Metric: "queue_depth", Labels: map[string]string{"queue": "background"}},
		{Metric: "queue_depth"},
		{Metric: "absent"},
	}
	for _, tc := range []struct {
		name, body string
		status     int
		wantError  string
	}{
		{"valid", "# TYPE queue_depth gauge\nqueue_depth{queue=\"background\"} 2\nqueue_depth{queue=\"critical\"} 3\n", 200, ""},
		{"parse failure", "invalid metric line\n", 200, "invalid Prometheus"},
		{"limited response", strings.Repeat("#", 2<<20+1), 200, "exceeds 2 MiB"},
		{"unavailable", "", 503, "status 503"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			values, err := scrape(t.Context(), server.Client(), server.URL, selectors)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("want error containing %q, got %v", tc.wantError, err)
				}
				return
			}
			if err != nil || len(values) != 3 || values[0] != (reading{2, 1, true}) || values[1] != (reading{5, 2, true}) || values[2].valid {
				t.Fatalf("unexpected readings %v, err %v", values, err)
			}
		})
	}
}

func TestRatioTransitions(t *testing.T) {
	threshold := 0.5
	_, evaluations := plan([]rule{{
		ID: "ratio", Targets: []string{"first", "second"}, Operation: "ratio",
		Select: selector{Metric: "used"}, Denominator: selector{Metric: "total"},
		Compare: ">", Threshold: &threshold, For: 2 * time.Second,
	}})
	data := func(used, total float64) map[string][]reading {
		return map[string][]reading{"first": {{used, 1, true}, {total, 1, true}}, "second": {{1, 1, true}, {1, 1, true}}}
	}
	start := time.Unix(100, 0)
	for _, tc := range []struct {
		at     int
		values map[string][]reading
		event  string
		known  bool
	}{
		{0, data(6, 8), "", true},
		{1, nil, "", false},
		{2, data(6, 8), "", true},
		{3, data(6, 8), "", true},
		{4, data(6, 8), "[FIRING]", true},
		{5, data(6, 8), "", true},
		{6, data(6, -1), "", false},
		{7, data(6, 8), "", true},
		{8, data(1, 8), "[RESOLVED]", true},
	} {
		message, known := evaluations[0].evaluate(start.Add(time.Duration(tc.at)*time.Second), tc.values)
		if known != tc.known || (tc.event == "" && message != "") || !strings.HasPrefix(message, tc.event) {
			t.Fatalf("at %d: got %q, known=%t", tc.at, message, known)
		}
	}
}

func TestNotifier(t *testing.T) {
	for _, tc := range []struct {
		name             string
		status, attempts int32
	}{
		{"success", 200, 1},
		{"permanent error", 403, 1},
		{"rate limited", 429, 2},
		{"transient error", 503, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]string
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["text"] != "test alert" {
					t.Errorf("unexpected Slack request: %v, %v", payload, err)
				}
				if attempts.Add(1) == 1 {
					w.Header().Set("Retry-After", "1")
					w.WriteHeader(int(tc.status))
				}
			}))
			defer server.Close()
			n := notifier{server.URL, server.Client(), make(chan string, 1)}
			n.enqueue("test alert")
			n.enqueue("should be dropped")
			if len(n.pending) != 1 {
				t.Fatal("expected queue capacity of one")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
			defer cancel()
			n.send(ctx, <-n.pending)
			if attempts.Load() != tc.attempts {
				t.Fatalf("got %d attempts, want %d", attempts.Load(), tc.attempts)
			}
		})
	}
}

func TestPrimaryHealth(t *testing.T) {
	var progress atomic.Pointer[time.Time]
	handler := healthHandler(&progress, time.Minute)
	fresh, expired := time.Now(), time.Now().Add(-2*time.Minute)
	for _, tc := range []struct {
		last *time.Time
		want int
	}{
		{nil, http.StatusServiceUnavailable},
		{&fresh, http.StatusOK},
		{&expired, http.StatusServiceUnavailable},
		{&fresh, http.StatusOK},
	} {
		progress.Store(tc.last)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if response.Code != tc.want {
			t.Fatalf("last progress %v: got %d, want %d", tc.last, response.Code, tc.want)
		}
	}
}

func TestScrapeCycleIsolatesTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, "sample 4\n")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	values := scrapeCycle(ctx, httpClient(300*time.Millisecond), []target{
		{ID: "slow", URL: server.URL + "/slow"},
		{ID: "fast", URL: server.URL + "/ok"},
	}, map[string][]selector{
		"slow": {{Metric: "sample"}},
		"fast": {{Metric: "sample"}},
	}, 0)
	if len(values) != 1 || len(values["fast"]) != 1 || values["fast"][0] != (reading{4, 1, true}) {
		t.Fatalf("expected readings from the fast target only, got %v", values)
	}
}
