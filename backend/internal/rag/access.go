package rag

// accessibleSpacesSQL is the parenthesized subquery (single column: space_id) of
// the spaces a user may retrieve from in ask/search: their own space_access PLUS
// every space published public (visibility='public').
//
// A public space is already world-readable — served unauthenticated through
// /api/public and SEO-indexed — so its chunks belong in the ask/search corpus for
// non-members too. Without this a signed-in user who isn't a member of the tela
// Docs space asks "how do I use the MCP?" and gets zero hits, because the docs are
// public yet outside their space_access. This grants READ for retrieval only:
// publishing still adds no space_access row and no write (the anti-leak invariant
// is preserved — we only union in content that is already public).
//
// uid must be the already-bound $N placeholder for the user id. It yields two
// columns — space_id and is_member (1 = the user has access via space_access,
// 0 = reachable only because the space is public). A space that is both collapses
// to is_member=1 (max), so membership always wins. Callers that only need the
// access filter join on space_id and ignore is_member; ranked surfaces read
// is_member to soft-demote public-only content below the user's own (see
// publicRankPenalty / publicDistPenalty) so a stranger's published space can't
// dilute member results on a near-tie.
func accessibleSpacesSQL(uid string) string {
	return `(SELECT space_id, max(is_member) AS is_member FROM (
	             SELECT space_id, 1 AS is_member FROM space_access WHERE user_id = ` + uid + `
	             UNION ALL SELECT id, 0 FROM spaces WHERE visibility = 'public'
	         ) a GROUP BY space_id)`
}

// Soft-demotion factors for public-only (non-member) content in ranked retrieval.
// Member content wins a near-tie, but a strongly-matching public doc still beats
// an irrelevant member page — so the "ask the public docs" path keeps working
// when the caller has no member match of their own.
const (
	publicRankPenalty = 0.5 // multiplies ts_rank (higher = better): halve public lexical scores
	publicDistPenalty = 1.4 // multiplies cosine distance (lower = better): push public farther
)
