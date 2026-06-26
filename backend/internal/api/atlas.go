package api

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"sync"

	"github.com/zcag/tela/backend/internal/atlas/core"
	atlasllm "github.com/zcag/tela/backend/internal/atlas/llm"
	atlasstore "github.com/zcag/tela/backend/internal/atlas/store"
	"github.com/zcag/tela/backend/internal/auth"
)

// atlasEmbedDim is the embedding width — the atlas_chunks (and page_chunks)
// vector(1024) column. Used for the zero-vector last-resort below.
const atlasEmbedDim = 1024

// atlasManager owns the lifecycle of documentation-generation runs. It builds
// each run's engine inputs from tela env + the atlas_sources/atlas_runs rows,
// drives the lifted engine in a goroutine, streams live progress over the hub,
// and resumes dangling runs on boot. One active run per source.
//
// It reuses tela's instance backends end-to-end: chat goes to the lifted atlas
// client pointed at TELA_LLM_URL; embedding is delegated to s.rag.Embed (same
// Ollama, same model) via the client's EmbedFunc seam; publishing writes pages
// directly (atlasPublisher) and queues s.rag reindex.
type atlasManager struct {
	s     *Server
	store *atlasstore.Store
	hub   *atlasHub

	mu     sync.Mutex
	active map[int64]context.CancelFunc // sourceID -> cancel for its in-flight run

	// paused is the admin AI kill-switch, shared with the other background
	// workers; the freshness scheduler skips a tick while it's true.
	paused func() bool
}

func newAtlasManager(s *Server) *atlasManager {
	return &atlasManager{
		s:      s,
		store:  atlasstore.New(s.DB),
		hub:    newAtlasHub(),
		active: map[int64]context.CancelFunc{},
	}
}

// atlasEnabled reports whether a run can start: both the embedder and the chat
// LLM must be configured, or generation produces ungrounded garbage.
func (m *atlasManager) atlasEnabled() bool {
	return m.s.rag.Enabled() && os.Getenv("TELA_LLM_URL") != ""
}

// atlasModelCfg builds the engine's ModelCfg from tela's instance env. Chat hits
// TELA_LLM_URL (already /v1) with the same transport atlas calibrated against;
// EmbedModel is only a stats label — embeddings actually flow through s.rag via
// the client's EmbedFunc (see embedBatch), so they use tela's exact embedder.
func atlasModelCfg(embedModel string) core.ModelCfg {
	return core.ModelCfg{
		BaseURL:    os.Getenv("TELA_LLM_URL"),
		APIKey:     os.Getenv("TELA_LLM_TOKEN"),
		ChatModel:  atlasGetenv("TELA_LLM_MODEL", "qwen2.5:7b"),
		EmbedModel: embedModel,
	}
}

// newLLMClient constructs the lifted client and wires embedding to tela's
// rag.Embedder, so generation embeds with the SAME instance embedder + model
// tela's own RAG uses — identical vectors, shared metering, one endpoint.
func (m *atlasManager) newLLMClient() *atlasllm.Client {
	c := atlasllm.New(atlasModelCfg(m.s.rag.EmbedModel()))
	c.EmbedFunc = m.embedBatch
	return c
}

// embedBatch is the EmbedFunc seam: it embeds each input through tela's
// rag.Embedder. Same contract as the client's built-in Embed — one vector per
// input, and the int is how many fell back to a zero vector. atlas's draft/embed
// stages drive this with bounded parallelism, so the shared Ollama stays calm.
// A per-item embed error degrades to a zero vector (the chunk just won't be
// dense-retrievable) rather than failing the whole run — mirroring atlas's
// last-resort. ctx cancellation propagates as a hard error.
func (m *atlasManager) embedBatch(ctx context.Context, inputs []string) ([][]float32, int, error) {
	out := make([][]float32, len(inputs))
	zeroed := 0
	for i, text := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		v, err := m.s.rag.Embed(ctx, text)
		if err != nil {
			out[i] = make([]float32, atlasEmbedDim)
			zeroed++
			continue
		}
		out[i] = v
	}
	return out, zeroed, nil
}

// atlasSpaceManageErr gates the management of atlas on a space (add/edit/delete
// sources, trigger runs, set cadence): the caller must be the space owner, or an
// admin of the org that owns the space, or an instance admin. Management is
// admin-level on purpose — a run fetches an external source, spends LLM budget,
// and rewrites the whole generated subtree. Mirrors membershipCore's bearer
// space-scope ceiling. Returns nil when allowed, else an *apiErr.
func (s *Server) atlasSpaceManageErr(ctx context.Context, u *auth.User, k *auth.APIKey, spaceID int64) *apiErr {
	if ae := apiKeySpaceScopeErr(k, spaceID); ae != nil {
		return ae
	}
	if u.IsInstanceAdmin {
		return nil
	}
	if role, err := spaceRole(ctx, s.DB, u.ID, spaceID); err == nil && role == roleOwner {
		return nil
	}
	// Org-owned space → org admins manage it.
	var orgID sql.NullInt64
	if err := s.DB.QueryRowContext(ctx, `SELECT org_id FROM spaces WHERE id = $1`, spaceID).Scan(&orgID); err != nil {
		if err == sql.ErrNoRows {
			return &apiErr{http.StatusNotFound, "not_found", "space not found"}
		}
		return &apiErr{http.StatusInternalServerError, "internal", "lookup space failed"}
	}
	if orgID.Valid {
		if r, err := orgRole(ctx, s.DB, u.ID, orgID.Int64); err == nil && r == orgRoleAdmin {
			return nil
		}
	}
	return &apiErr{http.StatusForbidden, "forbidden", "space management required"}
}

func atlasGetenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
