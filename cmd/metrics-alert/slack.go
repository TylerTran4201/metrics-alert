package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

type notifier struct {
	endpoint string
	client   *http.Client
	pending  chan string
}

func (n notifier) enqueue(message string) {
	select {
	case n.pending <- message:
	default:
		slog.Error("notification dropped", "reason", "queue full")
	}
}

func (n notifier) run(ctx context.Context) {
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case message := <-n.pending:
			n.send(ctx, message)
		}
	}
}

func (n notifier) send(ctx context.Context, message string) {
	payload, _ := json.Marshal(struct {
		Text string `json:"text"`
	}{message})
	for attempt := 1; attempt <= 3 && ctx.Err() == nil; attempt++ {
		code, retryAfter := n.post(ctx, payload)
		switch {
		case ctx.Err() != nil:
			return
		case code >= 200 && code < 300:
			slog.Info("notification delivered")
			return
		case code != 0 && code != http.StatusTooManyRequests && (code < 500 || code >= 600):
			slog.Error("notification rejected", "status", code)
			return
		}
		delay := time.Duration(attempt) * time.Second
		if code == http.StatusTooManyRequests {
			delay = max(delay, retryAfter)
		}
		if attempt < 3 || code == http.StatusTooManyRequests {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
	slog.Error("notification dropped", "reason", "retry limit")
}

func (n notifier) post(ctx context.Context, payload []byte) (int, time.Duration) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.endpoint, bytes.NewReader(payload))
	if err != nil {
		return http.StatusBadRequest, 0
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return 0, 0
	}
	defer resp.Body.Close()
	seconds, _ := strconv.ParseInt(resp.Header.Get("Retry-After"), 10, 64)
	return resp.StatusCode, time.Duration(min(max(seconds, 0), int64(time.Duration(1<<63-1)/time.Second))) * time.Second
}
