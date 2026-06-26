package core

import (
	"strings"
	"time"
)

// NotifyChannel is the transport a NotifyRoute dispatches to.
type NotifyChannel string

const (
	ChannelWebhook  NotifyChannel = "webhook"
	ChannelTelegram NotifyChannel = "telegram"
	ChannelEmail    NotifyChannel = "email"
)

// NotifyEvent is a run-finish condition a route can subscribe to. A route's
// Events field is a comma-separated subset of these.
type NotifyEvent string

const (
	EventDone        NotifyEvent = "done"
	EventFailed      NotifyEvent = "failed"
	EventDrift       NotifyEvent = "drift"
	EventLowCoverage NotifyEvent = "low_coverage"
)

// DefaultCoverageThreshold is the must-cover rate below which low_coverage fires
// when a route leaves CoverageThreshold unset.
const DefaultCoverageThreshold = 0.8

// NotifyRoute routes a concise run-finish summary to one channel. It belongs to a
// project (ProjectID; 0 would be a global route, deferred — MVP scopes per
// project). Events is a comma-separated subset of done|failed|drift|low_coverage;
// a route fires when its events intersect the run's fired events. Target is the
// channel address (webhook url | telegram chat_id | email addr); SecretID points
// at a unified Secret carrying the channel credential (telegram bot token, or a
// webhook HMAC signing key). CoverageThreshold gates low_coverage (default 0.8).
type NotifyRoute struct {
	ID                int64         `json:"id"`
	ProjectID         int64         `json:"project_id"`
	Events            string        `json:"events"` // comma-sep: done,failed,drift,low_coverage
	Channel           NotifyChannel `json:"channel"`
	Target            string        `json:"target"`
	SecretID          int64         `json:"secret_id"`
	Enabled           bool          `json:"enabled"`
	CoverageThreshold float64       `json:"coverage_threshold"`
	CreatedAt         time.Time     `json:"created_at"`
}

// Threshold returns the route's low_coverage cutoff, defaulting when unset.
func (r NotifyRoute) Threshold() float64 {
	if r.CoverageThreshold <= 0 {
		return DefaultCoverageThreshold
	}
	return r.CoverageThreshold
}

// WantsEvent reports whether the route subscribes to ev (Events is a comma-sep
// list; whitespace around entries is tolerated).
func (r NotifyRoute) WantsEvent(ev NotifyEvent) bool {
	for _, e := range strings.Split(r.Events, ",") {
		if NotifyEvent(strings.TrimSpace(e)) == ev {
			return true
		}
	}
	return false
}
