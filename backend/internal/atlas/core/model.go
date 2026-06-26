// Package core holds atlas's domain model: the entities and events that the
// engine, store, and UI all speak. It depends on nothing else in the tree.
package core

import "time"

// Connection is a reusable, named LLM backend (chat + embeddings). Projects
// reference one so endpoints/models live in one place and can be tested.
type Connection struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	BaseURL    string    `json:"base_url"`  // chat endpoint
	EmbedURL   string    `json:"embed_url"` // embeddings endpoint (falls back to base)
	APIKey     string    `json:"api_key"`
	ChatModel  string    `json:"chat_model"`
	EmbedModel string    `json:"embed_model"`
	// prices in USD per 1M tokens (0 = free, e.g. local). Cost is computed from a
	// run's Usage × these, so swapping to a priced cloud model fills cost in.
	InputPrice  float64 `json:"input_price"`
	OutputPrice float64 `json:"output_price"`
	EmbedPrice  float64 `json:"embed_price"`
	// SecretID references the unified Secret holding the API key. When non-zero it
	// supersedes the inline APIKey (resolved via internal/secret); the inline
	// APIKey stays as a migration-window fallback. 0 = use APIKey.
	SecretID  int64     `json:"secret_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Cost returns the USD cost of a run's token usage under this connection's prices.
func (c Connection) Cost(u Usage) float64 {
	return float64(u.PromptTokens)/1e6*c.InputPrice +
		float64(u.CompletionTokens)/1e6*c.OutputPrice +
		float64(u.EmbedTokens)/1e6*c.EmbedPrice
}

// Model resolves a Connection to the ModelCfg the engine consumes.
func (c Connection) Model() ModelCfg {
	return ModelCfg{BaseURL: c.BaseURL, EmbedURL: c.EmbedURL, APIKey: c.APIKey,
		ChatModel: c.ChatModel, EmbedModel: c.EmbedModel}
}

// Project is a named workspace. It owns sources and references a Connection +
// output target; runs write generated docs to its output directory.
//
// Freshness: AutoUpdate + Cadence drive the background refresh poller
// (internal/refresh). When AutoUpdate is on and the Cadence interval has elapsed
// since LastRefreshAt, the poller fires a change-gated delta sync. Cadence is one
// of the presets "hourly"|"daily"|"weekly"|"monthly" (""=off). LastRefreshAt is
// stamped after each successful poll fire (zero = never refreshed = due now).
type Project struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	OutputDir     string    `json:"output_dir"`
	ConnectionID  int64     `json:"connection_id"` // 0 = use inline Model fallback
	Model         ModelCfg  `json:"model"`         // fallback when ConnectionID==0
	AutoUpdate    bool      `json:"auto_update"`   // background refresh on/off
	Cadence       string    `json:"cadence"`       // "hourly"|"daily"|"weekly"|"monthly"|""=off
	LastRefreshAt time.Time `json:"last_refresh_at,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// ModelCfg is the LLM/embedding endpoints a project ingests against. Anything
// OpenAI-compatible (tardis/Ollama/…); the key may be a dummy for local servers.
// EmbedURL lets embeddings target a different server than chat — important when
// they're separate instances (tardis pins a dedicated embed Ollama so it never
// contends with the big chat model). Empty EmbedURL falls back to BaseURL.
type ModelCfg struct {
	BaseURL    string `json:"base_url"`  // chat endpoint, e.g. http://tardis:11439/v1
	EmbedURL   string `json:"embed_url"` // embeddings endpoint, e.g. http://tardis:11435/v1
	APIKey     string `json:"api_key"`   // dummy is fine for local
	ChatModel  string `json:"chat_model"`
	EmbedModel string `json:"embed_model"`
	// Concurrency caps total in-flight LLM requests (chat+embed) for a client.
	// 0 = the package default (overridable via ATLAS_LLM_CONCURRENCY). The gate
	// exists so parallel stages/sources never swamp a single-box provider.
	Concurrency int `json:"concurrency,omitempty"`
}

// EmbedBase returns the embeddings endpoint, falling back to the chat endpoint.
func (m ModelCfg) EmbedBase() string {
	if m.EmbedURL != "" {
		return m.EmbedURL
	}
	return m.BaseURL
}

// SourceType enumerates what an ingestion source can be. The engine treats this
// as an open set (selected via the connector registry) so new types slot in.
type SourceType string

const (
	SourceGit  SourceType = "git"
	SourceJira SourceType = "jira"
)

// Source is something to ingest, attached to a project. Location is a git URL
// or a local path; Ref pins a commit (filled in at acquire time). The optional
// scoping fields narrow what gets ingested: a branch, a subpath, and comma-
// separated include/exclude globs (supporting * and **).
type Source struct {
	ID        int64      `json:"id"`
	ProjectID int64      `json:"project_id"`
	Type      SourceType `json:"type"`
	Location  string     `json:"location"`
	Name      string     `json:"name"`    // optional display label for this source's subtree
	Ref       string     `json:"ref"`     // resolved commit sha
	Branch    string     `json:"branch"`  // optional: clone this branch
	Subpath   string     `json:"subpath"` // optional: only ingest under this path
	Include   string     `json:"include"` // optional: comma-separated globs to keep
	Exclude   string     `json:"exclude"` // optional: comma-separated globs to drop
	// SecretID references the unified Secret holding this source's access token
	// (e.g. a git/Jira token). 0 = none. The git connector ingests public/local
	// repos and doesn't consume it; the jira connector does.
	SecretID  int64     `json:"secret_id"`
	CreatedAt time.Time `json:"created_at"`
	// SecretValue/SecretMeta are the resolved secret, populated by the engine's
	// acquire stage (from SecretID via internal/secret) just before invoking the
	// connector — so the Connector interface stays auth-agnostic. Transient: never
	// persisted, never serialized over the API (json:"-").
	SecretValue string            `json:"-"`
	SecretMeta  map[string]string `json:"-"`
}

// Secret is the unified, named secret used everywhere a token/key lives: LLM
// connection keys, destination tokens, and source tokens all reference one by
// SecretID instead of embedding the value. This is the single store that
// internal/secret resolves against; the inline Connection.api_key /
// Destination.token remain only as fallbacks.
//
// Value is write-only: it carries inbound on create/update but is blanked on
// every read response so it never leaks over the API. Meta holds non-secret
// adornments (e.g. a tela base_url) so a kind can carry endpoint context. Value
// is plaintext at rest for now — encryption-at-rest is a later seam.
type Secret struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"` // unique
	Kind      string            `json:"kind"` // "llm" | "tela" | "git" | "jira" | "webhook"
	Value     string            `json:"value,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// Destination is an output target a project's docs are published to, in addition
// to the local folder. Type "tela" pushes pages into a wiki space. The token is
// resolved from the referenced Secret (SecretID), with the inline Token as a
// fallback.
type Destination struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"project_id"`
	Type      string `json:"type"`     // "tela"
	BaseURL   string `json:"base_url"` // tela: instance URL (from the secret's meta when unset)
	Token     string `json:"token,omitempty"`
	Space     string `json:"space"`  // tela: space name
	Parent    string `json:"parent"` // tela: top-dir page
	// SecretID references the unified Secret holding this destination's token. When
	// non-zero it supersedes the inline Token (resolved via internal/secret).
	SecretID  int64     `json:"secret_id"`
	CreatedAt time.Time `json:"created_at"`
}

// RunStatus is the lifecycle of a single ingestion run.
type RunStatus string

const (
	RunPending  RunStatus = "pending"
	RunRunning  RunStatus = "running"
	RunDone     RunStatus = "done"
	RunFailed   RunStatus = "failed"
	RunCanceled RunStatus = "canceled"
)

// RunKind distinguishes a full re-ingest from a delta (changed-inputs) run.
type RunKind string

const (
	RunFull  RunKind = "full"
	RunDelta RunKind = "delta"
)

// ChangeSet is the set of source units that changed between two refs, driving a
// delta run. Paths are source-relative (the connector applies the same scope
// filters as Inventory). It lives in core (not source) so core.Run can carry it
// without an import cycle; source.ChangeSet aliases this type.
type ChangeSet struct {
	Added    []string `json:"added,omitempty"`
	Modified []string `json:"modified,omitempty"`
	Deleted  []string `json:"deleted,omitempty"`
}

// Empty reports whether the changeset has no changes at all.
func (c ChangeSet) Empty() bool {
	return len(c.Added) == 0 && len(c.Modified) == 0 && len(c.Deleted) == 0
}

// Run is one execution of the pipeline over a source.
type Run struct {
	ID         int64      `json:"id"`
	SourceID   int64      `json:"source_id"`
	Kind       RunKind    `json:"kind"`                  // "full" (default) | "delta"
	BaselineID int64      `json:"baseline_id,omitempty"` // for delta: the prior run this builds on
	ChangeSet  *ChangeSet `json:"changeset,omitempty"`   // for delta: what changed since baseline
	Status     RunStatus  `json:"status"`
	Stage      StageName  `json:"stage"` // current/last stage
	Err        string     `json:"err,omitempty"`
	Coverage   *Coverage  `json:"coverage,omitempty"` // set once validate/repair run
	Stats      *RunStats  `json:"stats,omitempty"`    // set at publish
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt time.Time  `json:"finished_at,omitempty"`
}

// StageName is the canonical id of each pipeline stage. The ordered list in
// engine.DefaultStages defines the pipeline; the UI renders one card per stage.
type StageName string

const (
	StageAcquire   StageName = "acquire"   // clone/copy + pin commit
	StageInventory StageName = "inventory" // discover + classify files
	StageParse     StageName = "parse"     // AST per file
	StageSpine     StageName = "spine"     // deterministic surface inventory
	StageChunk     StageName = "chunk"     // symbol-aware chunks + line ranges
	StageEmbed     StageName = "embed"     // embed every chunk
	StageIndex     StageName = "index"     // vector + keyword + symbol graph
	StageOutline   StageName = "outline"   // plan the wiki (spine-seeded)
	StageDraft     StageName = "draft"     // grounded+cited per-page generation
	StageRefine    StageName = "refine"    // multi-pass critique/expand
	StageValidate  StageName = "validate"  // mermaid + citations + coverage audit
	StageRepair    StageName = "repair"    // regenerate gaps until threshold
	StagePublish   StageName = "publish"   // write doc tree + coverage + manifest
)

// EventLevel classifies a progress event.
type EventLevel string

const (
	LevelInfo  EventLevel = "info"
	LevelWarn  EventLevel = "warn"
	LevelError EventLevel = "error"
)

// Event is a single progress signal emitted during a run. Stages emit these as
// they work; the console printer (and later the web UI) render them live. Cur/
// Total drive progress bars; a zero Total means "no countable items, just a log".
type Event struct {
	RunID int64      `json:"run_id"`
	Stage StageName  `json:"stage"`
	Level EventLevel `json:"level"`
	Msg   string     `json:"msg"`
	Cur   int        `json:"cur"`
	Total int        `json:"total"`
	At    time.Time  `json:"at"`
}
