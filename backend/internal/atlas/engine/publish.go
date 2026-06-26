package engine

import "context"

// Publisher delivers a finished run's pages into the bound tela space. Standalone
// atlas pushed to a tela instance over a REST client (deliver.go); inside tela the
// implementation lives in internal/api and writes pages DIRECTLY via SQL (the
// summarize/agreement background-write pattern — no per-write user identity, no
// revision/notification/updated_at pollution), records the (source, slug)→page
// mapping in atlas_page_map for upsert-in-place, prunes orphans, and queues RAG
// reindex so the generated pages become searchable/askable.
//
// It is a RunContext seam (like EngineStore): the publish stage calls it after
// computing stats; a nil Publisher means "no delivery" (standalone CLI / tests).
// Publish reads everything it needs off rc: rc.Art.Pages (with linkified bodies +
// slugs), rc.Art.Spine, rc.Coverage, rc.Run.Stats, and rc.Source (id/name/
// location/ref). The space binding (space id, optional top-dir parent page) is
// captured when the executor constructs the implementation.
type Publisher interface {
	Publish(ctx context.Context, rc *RunContext) error
}
