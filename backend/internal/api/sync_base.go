package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/merge"
	"github.com/zcag/tela/backend/internal/models"
)

// sync_base.go — the per-client merge base (sync spec §5 B5) and the merge
// orchestration the WebDAV write path calls. The base is "what this client last
// had" for a page; ApplyFileSync diff3s the incoming file against the current DB
// row using it, so two independent edits combine instead of clobbering.

// davMergePrefer decides which side wins an unresolvable conflict hunk. Incoming
// (the local file edit) wins so the user's just-typed change stays visible; the
// overridden DB side is always snapshotted as a `sync-conflict` revision, so
// nothing is lost. Flip to merge.PreferCurrent to make the canonical store win.
const davMergePrefer = merge.PreferIncoming

// mergeMaxLineProduct caps the O(n*m) LCS so a pathologically large body can't
// stall a sync write. Past it we fall back to last-write-wins — the merge only
// ever ADDS safety, so skipping it degrades gracefully rather than hanging.
const mergeMaxLineProduct = 4_000_000

type syncBase struct {
	title string
	body  string
	props map[string]any
}

// loadSyncBaseTx fetches the client's stored base for a page within an open tx.
// ok=false means no base yet (first contact → caller falls back to last-write-wins).
func loadSyncBaseTx(ctx context.Context, tx *sql.Tx, apiKeyID, pageID int64) (syncBase, bool, error) {
	var (
		b   syncBase
		raw []byte
	)
	err := tx.QueryRowContext(ctx,
		`SELECT base_title, base_body, base_props FROM sync_base WHERE api_key_id = $1 AND page_id = $2`,
		apiKeyID, pageID).Scan(&b.title, &b.body, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return syncBase{}, false, nil
	}
	if err != nil {
		return syncBase{}, false, err
	}
	b.props = map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &b.props); err != nil {
			return syncBase{}, false, err
		}
	}
	return b, true, nil
}

// dbExec is satisfied by both *sql.DB and *sql.Tx so the base upsert works inside
// or outside a transaction.
type dbExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// upsertSyncBase records (title, body, props) as the client's base for a page —
// the state it most recently exchanged — so its next edit 3-way-merges against
// the right ancestor.
func upsertSyncBase(ctx context.Context, ex dbExec, apiKeyID, pageID int64, title, body string, props map[string]any) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO sync_base (api_key_id, page_id, base_title, base_body, base_props, updated_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, tela_now())
		ON CONFLICT (api_key_id, page_id) DO UPDATE
		   SET base_title = EXCLUDED.base_title,
		       base_body  = EXCLUDED.base_body,
		       base_props = EXCLUDED.base_props,
		       updated_at = tela_now()`,
		apiKeyID, pageID, title, body, propsJSON(props))
	return err
}

// insertSyncBaseIfAbsent records a base ONLY when the client has none for this
// page yet — used by the WebDAV read path. The first time a client downloads a
// page it never uploaded (e.g. one created in the app), this establishes the
// merge base so its first local edit 3-way-merges instead of last-write-wins.
// It deliberately does NOT overwrite an existing base: stock clients issue
// HEAD/GET probes during their own bookkeeping, and overwriting base on every
// read would set base==current right before a PUT and collapse the merge to LWW
// (the bug fixed in 66a8a51). A stale base is harmless — diff3 reconciles
// against any common ancestor, and the incoming PUT already carries whatever the
// client downloaded.
func insertSyncBaseIfAbsent(ctx context.Context, ex dbExec, apiKeyID, pageID int64, title, body string, props map[string]any) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO sync_base (api_key_id, page_id, base_title, base_body, base_props, updated_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, tela_now())
		ON CONFLICT (api_key_id, page_id) DO NOTHING`,
		apiKeyID, pageID, title, body, propsJSON(props))
	return err
}

// mergeAgainstBase three-way merges the incoming (title, body, props) against the
// current DB page using the client's stored base (loaded in-tx). With no client
// key, no stored base, or a body too large to diff safely, it returns the
// incoming values unchanged and conflicted=false — i.e. last-write-wins. Body is
// line-diff3'd, props are field-merged, title is a scalar merge (spec §5).
func (s *Server) mergeAgainstBase(ctx context.Context, tx *sql.Tx, k *auth.APIKey, cur models.Page, title, body string, props map[string]any) (mTitle, mBody string, mProps map[string]any, conflicted bool, err error) {
	mTitle, mBody, mProps = title, body, props
	if k == nil {
		return mTitle, mBody, mProps, false, nil
	}
	base, ok, err := loadSyncBaseTx(ctx, tx, k.ID, cur.ID)
	if err != nil {
		return mTitle, mBody, mProps, false, err
	}
	if !ok || mergeTooLarge(base.body, cur.Body, body) {
		return mTitle, mBody, mProps, false, nil
	}
	var (
		titleConflict bool
		bodyConflicts []merge.Conflict
		propConflicts []string
	)
	mTitle, titleConflict = merge.Scalar(base.title, cur.Title, title, davMergePrefer)
	mBody, bodyConflicts = merge.Merge3(base.body, cur.Body, body, davMergePrefer)
	mProps, propConflicts = merge.MergeProps(base.props, cur.Props, props, davMergePrefer)
	conflicted = titleConflict || len(bodyConflicts) > 0 || len(propConflicts) > 0
	return mTitle, mBody, mProps, conflicted, nil
}

func mergeTooLarge(base, current, incoming string) bool {
	lb := strings.Count(base, "\n") + 1
	lc := strings.Count(current, "\n") + 1
	li := strings.Count(incoming, "\n") + 1
	return lb*lc > mergeMaxLineProduct || lb*li > mergeMaxLineProduct
}
