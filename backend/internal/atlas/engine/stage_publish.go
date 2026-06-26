package engine

import (
	"context"
	"time"

	"github.com/zcag/tela/backend/internal/atlas/core"
)

// publishStage finalizes a run's pages and records run stats.
//
// Phase 1 form: it linkifies resolvable file:line citations into clickable source
// URLs and persists the linked bodies (so any reader serves linked citations, not
// bare ones — idempotent on delta re-runs), then saves RunStats (counts + token
// usage). The in-process delivery into the bound tela space (the per-source root +
// coverage overview + upsert-by-slug, ported from internal/atlas/_ref/deliver.go.ref)
// is wired in Phase 2 against tela's page core funcs; standalone atlas's local-folder
// writes + destination push are intentionally dropped (the space is the output).
type publishStage struct{}

func (publishStage) Name() core.StageName { return core.StagePublish }

func (publishStage) Run(ctx context.Context, rc *RunContext) error {
	for i := range rc.Art.Pages {
		p := &rc.Art.Pages[i]
		p.Body = linkifyCitations(*rc.Source, rc.Art.Files, p.Body)
		_ = rc.Store.UpdatePageBody(p.ID, p.Body)
	}

	cov := rc.Coverage
	stats := core.RunStats{
		Files: len(rc.Art.Files), Surface: len(rc.Art.Spine), Chunks: len(rc.Art.Chunks), Pages: len(rc.Art.Pages),
		DurationSec: time.Since(rc.Run.StartedAt).Seconds(),
		ChatModel:   rc.Project.Model.ChatModel, EmbedModel: rc.Project.Model.EmbedModel, Usage: rc.LLM.Usage(),
	}
	_ = rc.Store.SaveRunStats(rc.Run.ID, stats)
	rc.Run.Stats = &stats // so the publisher's overview can render run stats

	// Deliver into the bound tela space (per-source root + coverage overview +
	// upsert-by-slug, then queue RAG reindex). Non-fatal: the run's pages are
	// already persisted; a delivery failure warns and the run still completes.
	if rc.Publisher != nil {
		if err := rc.Publisher.Publish(ctx, rc); err != nil {
			rc.Warn("publish to space failed: %v", err)
		}
	}

	u := stats.Usage
	rc.Info("wrote %d pages (surface %.0f%%, must-cover %.0f%%) · %d chat calls (%dk in + %dk out) · %d embed calls (%dk)",
		len(rc.Art.Pages), 100*cov.Rate(), 100*cov.MustRate(),
		u.ChatCalls, u.PromptTokens/1000, u.CompletionTokens/1000, u.EmbedCalls, u.EmbedTokens/1000)
	return nil
}
