package main

import (
	"fmt"
	"time"
)

type alertState struct {
	since  time.Time
	firing bool
}

func (state *alertState) observe(now time.Time, known, active bool, hold time.Duration) string {
	if !known {
		state.since = time.Time{}
		return ""
	}
	if !active {
		state.since = time.Time{}
		if state.firing {
			state.firing = false
			return "RESOLVED"
		}
		return ""
	}
	if state.since.IsZero() {
		state.since = now
	}
	if !state.firing && now.Sub(state.since) >= hold {
		state.firing = true
		return "FIRING"
	}
	return ""
}

type input struct {
	target string
	index  int
}

type evaluation struct {
	rule                   rule
	scope                  string
	numerator, denominator []input
	state                  alertState
}

func plan(rules []rule) (map[string][]selector, []evaluation) {
	selectors := make(map[string][]selector)
	var evaluations []evaluation
	for _, r := range rules {
		e := evaluation{rule: r, scope: "configured targets"}
		for _, id := range r.Targets {
			numerator := input{id, len(selectors[id])}
			selectors[id] = append(selectors[id], r.Select)
			if r.Operation == "gauge" {
				evaluations = append(evaluations, evaluation{rule: r, scope: id, numerator: []input{numerator}})
				continue
			}
			e.numerator = append(e.numerator, numerator)
			if r.Operation == "ratio" {
				e.denominator = append(e.denominator, input{id, len(selectors[id])})
				selectors[id] = append(selectors[id], r.Denominator)
			}
		}
		if r.Operation != "gauge" {
			evaluations = append(evaluations, e)
		}
	}
	return selectors, evaluations
}

func sum(inputs []input, data map[string][]reading, unique bool) (float64, bool) {
	var total float64
	for _, in := range inputs {
		values := data[in.target]
		if len(values) <= in.index || !values[in.index].valid || (unique && values[in.index].count != 1) {
			return 0, false
		}
		total += values[in.index].total
	}
	return total, finite(total)
}

func compare(value float64, operator string, limit float64) bool {
	switch operator {
	case ">":
		return value > limit
	case ">=":
		return value >= limit
	case "<":
		return value < limit
	case "<=":
		return value <= limit
	case "==":
		return value == limit
	default:
		return value != limit
	}
}

func (e *evaluation) evaluate(now time.Time, data map[string][]reading) (string, bool) {
	value, known := sum(e.numerator, data, e.rule.Operation == "gauge")
	if known && e.rule.Operation == "ratio" {
		denominator, valid := sum(e.denominator, data, false)
		known = valid && denominator != 0
		if known {
			value /= denominator
			known = finite(value)
		}
	}
	event := e.state.observe(now, known, compare(value, e.rule.Compare, *e.rule.Threshold), e.rule.For)
	if event == "" {
		return "", known
	}
	message := fmt.Sprintf("[%s] %s (%s): value=%g, condition=%s %g", event, e.rule.ID, e.scope, value, e.rule.Compare, *e.rule.Threshold)
	if e.rule.Message != "" {
		message += " — " + e.rule.Message
	}
	return message, known
}
