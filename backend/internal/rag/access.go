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
// uid must be the already-bound $N placeholder for the user id. The UNION dedupes,
// so a space the user is also a member of isn't double-counted.
func accessibleSpacesSQL(uid string) string {
	return `(SELECT space_id FROM space_access WHERE user_id = ` + uid + `
	         UNION SELECT id FROM spaces WHERE visibility = 'public')`
}
