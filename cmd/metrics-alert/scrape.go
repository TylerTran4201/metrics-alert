package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

type reading struct {
	total float64
	count int
	valid bool
}

func scrape(ctx context.Context, client *http.Client, endpoint string, selectors []selector) ([]reading, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("invalid scrape request")
	}
	req.Header.Set("Accept", "text/plain; version=0.0.4")
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("scrape HTTP request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scrape HTTP status %d", resp.StatusCode)
	}
	const maxResponse = 2 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(raw) > maxResponse {
		return nil, errors.New("scrape response unreadable or exceeds 2 MiB")
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("invalid Prometheus text response")
	}
	series := 0
	for _, family := range families {
		series += len(family.Metric)
		if series > 20000 {
			return nil, errors.New("scrape response exceeds 20,000 metric instances")
		}
	}
	values := make([]reading, len(selectors))
	for i, s := range selectors {
		values[i] = reduce(families[s.Metric], s)
	}
	return values, nil
}

func reduce(family *dto.MetricFamily, s selector) reading {
	var result reading
	if family == nil {
		return result
	}
	for _, metric := range family.Metric {
		matched := 0
		for _, label := range metric.Label {
			if value, exists := s.Labels[label.GetName()]; exists && value == label.GetValue() {
				matched++
			}
		}
		if matched != len(s.Labels) {
			continue
		}
		var value float64
		switch {
		case metric.Gauge != nil:
			value = metric.Gauge.GetValue()
		case metric.Untyped != nil:
			value = metric.Untyped.GetValue()
		case metric.Counter != nil:
			value = metric.Counter.GetValue()
		default:
			return reading{}
		}
		if !finite(value) {
			return reading{}
		}
		result.total += value
		result.count++
	}
	result.valid = result.count > 0 && finite(result.total)
	return result
}
