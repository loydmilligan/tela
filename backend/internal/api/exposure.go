package api

import (
	"context"
	"database/sql"
)

// Public-exposure resolution. See docs/visibility-model.md.
//
// A page has no stored visibility — "who can read this internally" is the
// space's membership (Axis 1). This file computes the *outbound* exposure
// (Axis 2): whether a page is reachable by a public share link, derived at read
// time from active rows in share_links. A page is exposed when it has a direct
// share, or an ancestor has an include_descendants share. The state collapses
// to the most-open contributing share (public > password).

const (
	exposurePrivate  = "private"  // space members only — the resting state
	exposurePublic   = "public"   // an active open (no-password) link reaches it
	exposurePassword = "password" // reachable only via a password-protected link
)

// pageExposure is the resolved, read-only exposure of a page. Never persisted.
type pageExposure struct {
	State     string  `json:"state"`      // private | public | password
	Inherited bool    `json:"inherited"`  // exposure comes only from an ancestor's include-descendants share
	ExpiresAt *string `json:"expires_at"` // when exposure ends (latest contributing expiry); nil = never expires / not exposed
}

// shareFact is the minimal slice of an active share link the resolver needs.
type shareFact struct {
	includeDescendants bool
	isPublic           bool // password_hash IS NULL
	expiresAt          sql.NullString
}

// loadActiveShareFacts returns active (non-revoked, non-expired) share links for
// every page in spaceID, keyed by page id. One query.
func loadActiveShareFacts(ctx context.Context, db *sql.DB, spaceID int64) (map[int64][]shareFact, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT sl.page_id, sl.include_descendants,
		       CASE WHEN sl.password_hash IS NULL THEN 1 ELSE 0 END, sl.expires_at
		  FROM share_links sl
		  JOIN pages p ON p.id = sl.page_id
		 WHERE p.space_id = ?
		   AND sl.revoked_at IS NULL
		   AND (sl.expires_at IS NULL OR sl.expires_at > datetime('now'))`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64][]shareFact{}
	for rows.Next() {
		var (
			pageID      int64
			includeDesc int
			isPublic    int
			expiresAt   sql.NullString
		)
		if err := rows.Scan(&pageID, &includeDesc, &isPublic, &expiresAt); err != nil {
			return nil, err
		}
		out[pageID] = append(out[pageID], shareFact{
			includeDescendants: includeDesc != 0,
			isPublic:           isPublic != 0,
			expiresAt:          expiresAt,
		})
	}
	return out, rows.Err()
}

// loadSpaceParentMap returns id -> parent id (nil for roots) for every page in
// spaceID. Used to walk ancestor chains when resolving inherited exposure.
func loadSpaceParentMap(ctx context.Context, db *sql.DB, spaceID int64) (map[int64]*int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, parent_id FROM pages WHERE space_id = ?`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	parent := map[int64]*int64{}
	for rows.Next() {
		var id int64
		var pid sql.NullInt64
		if err := rows.Scan(&id, &pid); err != nil {
			return nil, err
		}
		if pid.Valid {
			v := pid.Int64
			parent[id] = &v
		} else {
			parent[id] = nil
		}
	}
	return parent, rows.Err()
}

// resolveExposures computes the exposure of every page in parent given the
// active share facts. Pure: no I/O, so it's the single tested core that both
// the tree build (in-memory parent map) and the single-page path feed into.
func resolveExposures(parent map[int64]*int64, shares map[int64][]shareFact) map[int64]pageExposure {
	out := make(map[int64]pageExposure, len(parent))
	for id := range parent {
		out[id] = resolveOneExposure(id, parent, shares)
	}
	return out
}

func resolveOneExposure(id int64, parent map[int64]*int64, shares map[int64][]shareFact) pageExposure {
	var (
		state            string
		inheritedOnly    = true
		hasAny           bool
		hasNeverExpiring bool
		maxExpiry        string
	)
	consider := func(f shareFact, direct bool) {
		hasAny = true
		if direct {
			inheritedOnly = false
		}
		if f.isPublic {
			state = exposurePublic
		} else if state != exposurePublic {
			state = exposurePassword
		}
		// Exposure ends when the LAST contributing share expires; a single
		// never-expiring contributor means it never ends.
		if !f.expiresAt.Valid {
			hasNeverExpiring = true
		} else if f.expiresAt.String > maxExpiry {
			maxExpiry = f.expiresAt.String
		}
	}

	for _, f := range shares[id] {
		consider(f, true)
	}
	// Walk ancestors; only include_descendants shares cascade down. seen guards
	// against a malformed cycle in parent_id.
	seen := map[int64]bool{id: true}
	for cur := parent[id]; cur != nil && !seen[*cur]; cur = parent[*cur] {
		seen[*cur] = true
		for _, f := range shares[*cur] {
			if f.includeDescendants {
				consider(f, false)
			}
		}
	}

	if !hasAny {
		return pageExposure{State: exposurePrivate}
	}
	exp := pageExposure{State: state, Inherited: inheritedOnly}
	if !hasNeverExpiring && maxExpiry != "" {
		v := maxExpiry
		exp.ExpiresAt = &v
	}
	return exp
}

// resolveSpaceExposures loads the parent map + active shares for spaceID and
// resolves every page's exposure. Used by the flat page list and single-page
// GET; the tree build reuses resolveExposures directly off its in-memory nodes.
func resolveSpaceExposures(ctx context.Context, db *sql.DB, spaceID int64) (map[int64]pageExposure, error) {
	parent, err := loadSpaceParentMap(ctx, db, spaceID)
	if err != nil {
		return nil, err
	}
	shares, err := loadActiveShareFacts(ctx, db, spaceID)
	if err != nil {
		return nil, err
	}
	return resolveExposures(parent, shares), nil
}

// resolvePageExposure resolves the exposure of a single page.
func resolvePageExposure(ctx context.Context, db *sql.DB, pageID, spaceID int64) (pageExposure, error) {
	m, err := resolveSpaceExposures(ctx, db, spaceID)
	if err != nil {
		return pageExposure{}, err
	}
	if e, ok := m[pageID]; ok {
		return e, nil
	}
	return pageExposure{State: exposurePrivate}, nil
}
