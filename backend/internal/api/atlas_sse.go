package api

import (
	"sync"

	atlascore "github.com/zcag/tela/backend/internal/atlas/core"
)

// atlasHub is the live progress fan-out for documentation-generation runs:
// stage events emitted by the engine are published here and streamed to any
// SSE subscribers watching that run. It is the live tier only — every event is
// ALSO persisted to atlas_run_events by the engine (via the EngineStore), so a
// subscriber that connects mid-run replays history from the DB first, then
// attaches here for the tail. Ported from standalone atlas's server/hub.go.
//
// Publish is non-blocking: a slow subscriber's buffered channel is allowed to
// drop events rather than stall the run (the DB remains the complete record).
type atlasHub struct {
	mu   sync.Mutex
	subs map[int64]map[chan atlascore.Event]struct{} // runID -> set of subscriber channels
}

func newAtlasHub() *atlasHub {
	return &atlasHub{subs: map[int64]map[chan atlascore.Event]struct{}{}}
}

// publish delivers e to every current subscriber of e.RunID, never blocking.
func (h *atlasHub) publish(e atlascore.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[e.RunID] {
		select {
		case ch <- e:
		default: // subscriber is behind; drop (DB has the full log)
		}
	}
}

// subscribe registers a buffered channel for runID's events and returns it plus
// an unsubscribe func that removes and closes it. Caller must call unsubscribe.
func (h *atlasHub) subscribe(runID int64) (<-chan atlascore.Event, func()) {
	ch := make(chan atlascore.Event, 256)
	h.mu.Lock()
	if h.subs[runID] == nil {
		h.subs[runID] = map[chan atlascore.Event]struct{}{}
	}
	h.subs[runID][ch] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs[runID], ch)
			if len(h.subs[runID]) == 0 {
				delete(h.subs, runID)
			}
			h.mu.Unlock()
			close(ch)
		})
	}
}
