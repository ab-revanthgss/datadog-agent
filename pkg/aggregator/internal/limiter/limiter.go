// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package limiter

import (
	"math"
	"strings"

	"github.com/DataDog/datadog-agent/pkg/metrics"
	"github.com/DataDog/datadog-agent/pkg/tagset"
)

type entry struct {
	current  int // number of contexts currently in aggregator
	rejected int // number of rejected samples
	lastSeen int // epoch count when seen last
	tags     []string
}

// Limiter tracks number of contexts based on origin detection metrics
// and rejects samples if the number goes over the limit.
//
// Not thread safe.
type Limiter struct {
	key     string
	tags    []string
	limit   int // current per-origin limit (dynamic if global limit is set)
	global  int // global limit
	current int // sum(usage[*].current)
	usage   map[string]*entry

	// epoch, maxAge and lastSeen ensure eventual removal of entries that created an entry, but were
	// never able to create contexts due to the global limit.
	epoch  int
	maxAge int
}

// New returns a limiter with a per-key limit.
//
// If limit is zero or less the limiter is disabled.
func New(limit int, key string, tags []string) *Limiter {
	if limit <= 0 {
		return nil
	}

	return newLimiter(limit, math.MaxInt, 0, key, tags)
}

// NewGlobal returns a limiter with a global per-instance limit, that
// will be equally distributed between origins.
func NewGlobal(global int, maxAge int, key string, tags []string) *Limiter {
	if global <= 0 || global == math.MaxInt {
		return nil
	}

	return newLimiter(0, global, maxAge, key, tags)
}

func newLimiter(limit, global int, maxAge int, key string, tags []string) *Limiter {
	if !strings.HasSuffix(key, ":") {
		key += ":"
	}

	hasKey := false
	tags = append([]string{}, tags...)
	for i := range tags {
		if !strings.HasSuffix(tags[i], ":") {
			tags[i] += ":"
		}
		hasKey = hasKey || key == tags[i]
	}

	if !hasKey {
		tags = append(tags, key)
	}

	return &Limiter{
		key:    key,
		tags:   tags,
		limit:  limit,
		global: global,
		usage:  map[string]*entry{},
		maxAge: maxAge,
	}
}

func (l *Limiter) identify(tags []string) string {
	for _, t := range tags {
		if strings.HasPrefix(t, l.key) {
			return t
		}
	}
	return ""
}

func (l *Limiter) extractTags(src []string) []string {
	dst := make([]string, 0, len(l.tags))

	for _, t := range src {
		for _, p := range l.tags {
			if strings.HasPrefix(t, p) {
				dst = append(dst, t)
			}
		}
	}

	return dst
}

func (l *Limiter) updateLimit() {
	if l.global < math.MaxInt && len(l.usage) > 0 {
		l.limit = l.global / len(l.usage)
	}
}

// Track is called for each new context. Returns true if the sample should be accepted, false
// otherwise.
func (l *Limiter) Track(tags []string) bool {
	if l == nil {
		return true
	}

	id := l.identify(tags)

	e := l.usage[id]
	if e == nil {
		e = &entry{
			tags: l.extractTags(tags),
		}
		l.usage[id] = e
		l.updateLimit()
	}

	e.lastSeen = l.epoch

	if e.current >= l.limit || l.current >= l.global {
		e.rejected++
		return false
	}

	l.current++
	e.current++
	return true
}

// Remove is called when context is expired to decrement current usage.
func (l *Limiter) Remove(tags []string) {
	if l == nil {
		return
	}

	id := l.identify(tags)

	if e := l.usage[id]; e != nil {
		l.current--
		e.current--
		if e.current <= 0 {
			delete(l.usage, id)
			l.updateLimit()
		}
	}
}

// IsOverLimit returns true if the context sender is over the limit and the context should be
// dropped.
func (l *Limiter) IsOverLimit(tags []string) bool {
	if l == nil {
		return false
	}

	if e := l.usage[l.identify(tags)]; e != nil {
		return e.current > l.limit
	}

	return false
}

// ExpireEntries is called once per flush cycle to do internal bookkeeping and cleanups.m
func (l *Limiter) ExpireEntries() {
	if l == nil {
		return
	}

	if l.maxAge >= 0 {
		l.epoch++
		tooOld := l.epoch - l.maxAge
		for id, e := range l.usage {
			if e.current == 0 && e.lastSeen < tooOld {
				delete(l.usage, id)
				l.updateLimit()
			}
		}
	}
}

// SendTelemetry appends limiter metrics to the series sink.
func (l *Limiter) SendTelemetry(timestamp float64, series metrics.SerieSink, hostname string, constTags []string) {
	if l == nil {
		return
	}

	droppedTags := append([]string{}, constTags...)
	droppedTags = append(droppedTags, "reason:too_many_contexts")

	series.Append(&metrics.Serie{
		Name:   "datadog.agent.aggregator.dogstatsd_context_limiter.num_origins",
		Host:   hostname,
		Tags:   tagset.NewCompositeTags(constTags, nil),
		MType:  metrics.APIGaugeType,
		Points: []metrics.Point{{Ts: timestamp, Value: float64(len(l.usage))}},
	})

	if l.global < math.MaxInt {
		series.Append(&metrics.Serie{
			Name:   "datadog.agent.aggregator.dogstatsd_context_limiter.global_limit",
			Host:   hostname,
			Tags:   tagset.NewCompositeTags(constTags, nil),
			MType:  metrics.APIGaugeType,
			Points: []metrics.Point{{Ts: timestamp, Value: float64(l.global)}},
		})
	}

	for _, e := range l.usage {
		series.Append(&metrics.Serie{
			Name:   "datadog.agent.aggregator.dogstatsd_context_limiter.limit",
			Host:   hostname,
			Tags:   tagset.NewCompositeTags(constTags, e.tags),
			MType:  metrics.APIGaugeType,
			Points: []metrics.Point{{Ts: timestamp, Value: float64(l.limit)}},
		})

		series.Append(&metrics.Serie{
			Name:   "datadog.agent.aggregator.dogstatsd_context_limiter.current",
			Host:   hostname,
			Tags:   tagset.NewCompositeTags(constTags, e.tags),
			MType:  metrics.APIGaugeType,
			Points: []metrics.Point{{Ts: timestamp, Value: float64(e.current)}},
		})

		series.Append(&metrics.Serie{
			Name:   "datadog.agent.aggregator.dogstatsd_samples_dropped",
			Host:   hostname,
			Tags:   tagset.NewCompositeTags(droppedTags, e.tags),
			MType:  metrics.APICountType,
			Points: []metrics.Point{{Ts: timestamp, Value: float64(e.rejected)}},
		})
		e.rejected = 0
	}
}
