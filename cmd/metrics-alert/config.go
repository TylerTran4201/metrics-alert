package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"time"

	"go.yaml.in/yaml/v3"
)

type target struct {
	ID  string
	URL string
}

type selector struct {
	Metric string
	Labels map[string]string
}

type rule struct {
	ID          string
	Targets     []string
	Operation   string
	Select      selector
	Denominator selector
	Compare     string
	Threshold   *float64
	For         time.Duration
	Message     string
}

type config struct {
	Interval     time.Duration
	Timeout      time.Duration
	HealthMaxAge time.Duration `yaml:"health_max_age"`
	Targets      []target
	Rules        []rule
}

func validURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func loadConfig(path string) (config, error) {
	cfg := config{Interval: 30 * time.Second, Timeout: 5 * time.Second, HealthMaxAge: 90 * time.Second}
	file, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer file.Close()
	const maxConfig = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(file, maxConfig+1))
	if err != nil || len(raw) > maxConfig {
		return cfg, errors.New("configuration unreadable or exceeds 1 MiB")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return cfg, errors.New("configuration must contain exactly one YAML document")
	}
	return cfg, cfg.validate()
}

func (cfg config) validate() error {
	if cfg.Timeout <= 0 || cfg.Interval <= cfg.Timeout || cfg.HealthMaxAge-cfg.Interval <= cfg.Timeout || cfg.HealthMaxAge <= cfg.Interval {
		return errors.New("require 0 < timeout < interval and health_max_age > interval + timeout")
	}
	if len(cfg.Targets) == 0 || len(cfg.Targets) > 64 || len(cfg.Rules) == 0 || len(cfg.Rules) > 128 {
		return errors.New("configure 1–64 targets and 1–128 rules")
	}
	targets := make(map[string]bool)
	for i, t := range cfg.Targets {
		if t.ID == "" || targets[t.ID] || !validURL(t.URL) {
			return fmt.Errorf("targets[%d] requires a unique id and an HTTP(S) url", i)
		}
		targets[t.ID] = true
	}
	rules := make(map[string]bool)
	for i, r := range cfg.Rules {
		if r.ID == "" || rules[r.ID] || r.Select.Metric == "" || r.Threshold == nil || !finite(*r.Threshold) || r.For < 0 {
			return fmt.Errorf("rules[%d] requires a unique id, select.metric, finite threshold, and nonnegative for", i)
		}
		rules[r.ID] = true
		switch r.Operation {
		case "gauge", "sum":
			if r.Denominator.Metric != "" || len(r.Denominator.Labels) > 0 {
				return fmt.Errorf("rule %q only accepts denominator with ratio", r.ID)
			}
		case "ratio":
			if r.Denominator.Metric == "" {
				return fmt.Errorf("rule %q requires denominator.metric", r.ID)
			}
		default:
			return fmt.Errorf("rule %q has an invalid operation", r.ID)
		}
		switch r.Compare {
		case ">", ">=", "<", "<=", "==", "!=":
		default:
			return fmt.Errorf("rule %q has an invalid compare operator", r.ID)
		}
		seen := make(map[string]bool)
		for _, id := range r.Targets {
			if !targets[id] || seen[id] {
				return fmt.Errorf("rule %q has an unknown or duplicate target %q", r.ID, id)
			}
			seen[id] = true
		}
		if len(seen) == 0 {
			return fmt.Errorf("rule %q requires targets", r.ID)
		}
	}
	return nil
}
