// Package source defines the typed source-connector seam. A Connector owns the
// source-specific front of the pipeline — acquire (materialize at a pinned ref),
// inventory (enumerate units), spine (the surface inventory) — while everything
// from chunk→publish stays source-agnostic in the engine.
//
// The interface uses neutral types (no engine import) so connectors stay pure
// source logic: the engine adapts its RunContext to these calls and keeps the
// store/emit/progress concerns to itself.
package source

import (
	"context"

	"github.com/zcag/tela/backend/internal/atlas/core"
)

// Snapshot is an acquired source materialized at a pinned ref. Dir is the
// on-disk root the connector works against; Ref is the resolved revision id
// (a commit sha for git).
type Snapshot struct {
	Dir string
	Ref string
}

// Progress is an optional per-unit progress callback (1-based index, total).
type Progress func(i1, total int)

// ChangeSet is the set of in-scope units that changed between two refs. It
// aliases core.ChangeSet so the run model can carry it without core importing
// source (which would cycle).
type ChangeSet = core.ChangeSet

// InventoryReport carries the skip breakdown of an inventory pass so the engine
// can reproduce its progress/summary logs without re-deriving source specifics.
type InventoryReport struct {
	Tracked       int // total candidate units scanned (e.g. tracked paths)
	Binary, Empty int // skipped: binary / empty
	Scoped        int // skipped by subpath/include/exclude scoping
	Langs         int // distinct languages kept
}

// ProgressConnector is an optional upgrade a Connector may implement to drive
// fine-grained progress and report skip stats. The engine uses it when present
// and otherwise falls back to the plain Inventory/Spine methods.
type ProgressConnector interface {
	InventoryWithProgress(ctx context.Context, snap Snapshot, src core.Source, onScan func(tracked int), onUnit Progress) ([]core.File, InventoryReport, error)
	SpineWithProgress(ctx context.Context, snap Snapshot, files []core.File, onUnit Progress) ([]core.SpineItem, error)
}

// Connector is one source type's front of the pipeline. Implementations are
// pure source logic: they read the source and return domain values; they do not
// touch persistence or progress emission (the engine owns those).
type Connector interface {
	// Type is the source-type id this connector handles ("git" | "jira" | …).
	Type() string
	// Acquire materializes the source into workdir and pins the exact ref.
	Acquire(ctx context.Context, src core.Source, workdir string) (Snapshot, error)
	// Inventory enumerates the in-scope, classified units (files, …).
	Inventory(ctx context.Context, snap Snapshot, src core.Source) ([]core.File, error)
	// Spine builds the deterministic surface inventory from the inventoried units.
	Spine(ctx context.Context, snap Snapshot, files []core.File) ([]core.SpineItem, error)
	// Delta reports the in-scope units that changed between two refs (fromRef →
	// toRef), applying the same scope filters Inventory uses. snap is the acquired
	// source materialized at toRef (its Dir is where the connector diffs).
	Delta(ctx context.Context, snap Snapshot, src core.Source, fromRef, toRef string) (ChangeSet, error)
	// HasChanges is a cheap, no-materialize probe: does the source have anything
	// newer than fromRef? Used by the refresh poller to skip cloning/fetching when
	// nothing changed. An empty fromRef means "no baseline" ⇒ always true (treat as
	// changed). git compares ls-remote HEAD to fromRef; jira counts issues updated
	// since fromRef. src carries the resolved secret (SecretValue/SecretMeta) so the
	// probe can authenticate, the same way Acquire does.
	HasChanges(ctx context.Context, src core.Source, fromRef string) (bool, error)
}
