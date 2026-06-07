package api

import (
	"strconv"

	"github.com/zcag/tela/backend/internal/pagemd"
)

// Cosmetic page slugs (docs/visibility-model.md → "Confluence-style" URLs).
// The slug derivation itself lives in package pagemd (the pure codec, shared
// with the markdown round-trip); these are the api-layer URL builders on top.
// A slug is NEVER canonical — the page id (or share token) is — so a stale or
// absent slug always still resolves.

// pageSlug derives a URL-safe slug from a title. Thin alias over pagemd.Slug.
func pageSlug(title string) string { return pagemd.Slug(title) }

// pagePermalinkPath returns the canonical public permalink path for a page:
// /p/{id}/{slug}, or bare /p/{id} when the title yields no slug.
func pagePermalinkPath(id int64, title string) string {
	base := "/p/" + strconv.FormatInt(id, 10)
	if s := pageSlug(title); s != "" {
		return base + "/" + s
	}
	return base
}

// pageAppPath returns the in-app SPA route path for a page:
// /spaces/{spaceID}/pages/{id}/{slug}, or without the slug suffix when the
// title yields none. Mirrors the frontend's pagePath (src/lib/slug.ts).
func pageAppPath(spaceID, id int64, title string) string {
	base := "/spaces/" + strconv.FormatInt(spaceID, 10) + "/pages/" + strconv.FormatInt(id, 10)
	if s := pageSlug(title); s != "" {
		return base + "/" + s
	}
	return base
}
