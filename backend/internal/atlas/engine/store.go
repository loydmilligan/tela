package engine

import "github.com/zcag/tela/backend/internal/atlas/core"

// EngineStore is the narrow persistence seam the pipeline needs. Standalone atlas
// backed this with a concrete SQLite *store.Store; inside tela it is implemented
// against Postgres over the run-scoped atlas_* tables (see internal/api). Keeping
// it an interface is the single change the lifted stages require — everything
// else (chunk sizes, retrieval fusion, prompts, thresholds) ports verbatim.
//
// The method set is exactly what the remaining engine files call: the per-stage
// writes, the resume read-backs (RunFiles/RunSpine/RunChunksWithVectors/
// RunPagesFull, rebuilding rc.Art + the in-memory Retriever), and the delta-reuse
// copy. Connection/destination-shaped calls are gone: the LLM ModelCfg is injected
// from tela env at run construction and the publish target is the bound space.
type EngineStore interface {
	// progress + run lifecycle
	AppendEvent(e core.Event) error
	UpdateRun(r *core.Run) error
	GetRun(id int64) (*core.Run, error)
	SetSourceRef(id int64, ref string) error

	// per-stage artifact writes
	SaveFiles(runID int64, files []core.File) error
	SaveSpine(runID int64, items []core.SpineItem) error
	SaveChunks(runID int64, chunks []core.Chunk) error
	SaveVectors(chunks []core.Chunk) error
	SavePages(runID int64, pages []core.Page) error
	UpdatePageBody(pageID int64, body string) error
	SaveRunCoverage(runID int64, c core.Coverage) error
	SaveRunStats(runID int64, st core.RunStats) error

	// delta reuse: copy unchanged baseline chunks (with vectors) into this run
	CopyChunksToRun(fromRunID, toRunID int64, files []string) (int, error)

	// resume read-backs: rehydrate rc.Art + rebuild the Retriever after a restart
	RunFiles(runID int64) ([]core.File, error)
	RunSpine(runID int64) ([]core.SpineItem, error)
	RunChunksWithVectors(runID int64) ([]core.Chunk, error)
	RunPagesFull(runID int64) ([]core.Page, error)
}
