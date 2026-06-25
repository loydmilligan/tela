package api

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// AI-health probing makes the host-context `ai_available` flag reflect the
// backing AI services' ACTUAL reachability, not just whether they're configured.
// A background prober pings the embedder (+ the chat model, when configured) on
// a fixed interval and caches the verdict; the user-facing "AI unavailable"
// header flips automatically when the model is down and clears on its own when
// it returns — no admin kill-switch toggle needed.
//
// Why a cached background probe and not a per-request or /health-time ping: the
// probes hit cheap metadata endpoints (model lists), never inference, so a cold
// local model is never woken; running them on a timer (not in a hot path) keeps
// the cost to one tiny request per service per interval regardless of traffic.
// This is the proactive complement to the per-request degrade in the ask UI (a
// failed completion still surfaces "the model didn't respond"); the prober just
// gets the header right *before* the user asks.

const (
	aiProbeInterval = 30 * time.Second
	aiProbeTimeout  = 5 * time.Second
)

// aiHealthState caches the latest probe outcome. checked stays false until the
// first probe completes, so aiHealthy() can report optimistically at boot (and
// in tests, which never start the prober) instead of flapping the header off.
type aiHealthState struct {
	mu      sync.RWMutex
	checked bool
	healthy bool
	reason  string
}

// aiHealthy reports whether AI should be advertised as available to clients. It
// requires AI to be enabled (configured + not admin-disabled) AND, once the
// prober has a verdict, the backing services to be reachable. Before the first
// probe — or when no prober runs at all (tests) — it trusts aiEnabled(), so
// behaviour matches the pre-probe world.
func (s *Server) aiHealthy() bool {
	if !s.aiEnabled() {
		return false
	}
	s.aiHealth.mu.RLock()
	defer s.aiHealth.mu.RUnlock()
	if !s.aiHealth.checked {
		return true
	}
	return s.aiHealth.healthy
}

// StartAIHealthProbe launches the background prober. Called once from the real
// entrypoint (cmd/tela), NOT from New(), so test servers never spawn it or hit
// the network — they fall through to aiHealthy()'s optimistic default.
func (s *Server) StartAIHealthProbe(ctx context.Context) {
	go func() {
		t := time.NewTicker(aiProbeInterval)
		defer t.Stop()
		s.probeAI(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.probeAI(ctx)
			}
		}
	}()
}

// probeAI runs one round: skip the network entirely when AI is off (unconfigured
// or admin kill-switch), else ping the embedder and — when configured — the chat
// model, recording the first failure as the reason. Both must be reachable for a
// healthy verdict, since "ask" needs both.
func (s *Server) probeAI(ctx context.Context) {
	if !s.aiEnabled() {
		s.setAIHealth(false, "ai disabled")
		return
	}
	pctx, cancel := context.WithTimeout(ctx, aiProbeTimeout)
	defer cancel()
	if err := s.rag.Ping(pctx); err != nil {
		s.setAIHealth(false, "embedder: "+err.Error())
		return
	}
	if s.llm.Enabled() {
		if err := s.llm.Ping(pctx); err != nil {
			s.setAIHealth(false, "chat: "+err.Error())
			return
		}
	}
	s.setAIHealth(true, "")
}

// setAIHealth stores the outcome and logs only on a state change (incl. the
// first verdict), so the log shows AI going down/up without per-tick noise.
func (s *Server) setAIHealth(ok bool, reason string) {
	s.aiHealth.mu.Lock()
	changed := !s.aiHealth.checked || s.aiHealth.healthy != ok
	s.aiHealth.checked = true
	s.aiHealth.healthy = ok
	s.aiHealth.reason = reason
	s.aiHealth.mu.Unlock()
	if !changed {
		return
	}
	if ok {
		slog.Info("ai health: available")
	} else {
		slog.Warn("ai health: unavailable", "reason", reason)
	}
}
