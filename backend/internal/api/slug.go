package api

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Cosmetic page slugs (docs/visibility-model.md → "Confluence-style" URLs).
//
// A slug is derived from the page title for human-readable URLs like
// /p/{id}/{slug}. It is NEVER canonical — the page id (or share token) is — so
// a stale or absent slug always still resolves. The frontend ships the same
// logic (src/lib/slug.ts); keep the two in sync so the address bar doesn't
// flicker between a backend-emitted slug (og:url, share links) and the
// FE-canonicalised one.

const maxSlugLen = 60

// slugTranslit maps the accented letters tela actually sees (Turkish + common
// Latin diacritics) to ASCII. Anything not here and not [a-z0-9] is dropped —
// emoji / CJK titles slug to "" and callers fall back to the bare /p/{id}.
var slugTranslit = map[rune]string{
	'ç': "c", 'Ç': "c", 'ğ': "g", 'Ğ': "g", 'ı': "i", 'İ': "i",
	'ö': "o", 'Ö': "o", 'ş': "s", 'Ş': "s", 'ü': "u", 'Ü': "u",
	'à': "a", 'á': "a", 'â': "a", 'ä': "a", 'ã': "a", 'å': "a",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o",
	'ù': "u", 'ú': "u", 'û': "u",
	'ñ': "n", 'Ñ': "n", 'ß': "ss", 'æ': "ae", 'œ': "oe",
}

var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// pageSlug derives a URL-safe, lowercase, hyphen-joined slug from a title.
// Truncates at a word ('-') boundary to <= maxSlugLen, and returns "" when
// nothing usable remains.
func pageSlug(title string) string {
	var b strings.Builder
	for _, r := range title {
		if sub, ok := slugTranslit[r]; ok {
			b.WriteString(sub)
		} else {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	s := strings.Trim(slugNonAlnum.ReplaceAllString(b.String(), "-"), "-")
	if len(s) > maxSlugLen {
		s = s[:maxSlugLen]
		if i := strings.LastIndexByte(s, '-'); i > 0 {
			s = s[:i]
		}
		s = strings.Trim(s, "-")
	}
	return s
}

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
// title yields none. Mirrors the frontend's pagePath (src/lib/slug.ts) — the
// page route lives under the space, so a bare /pages/{id} no longer resolves.
func pageAppPath(spaceID, id int64, title string) string {
	base := "/spaces/" + strconv.FormatInt(spaceID, 10) + "/pages/" + strconv.FormatInt(id, 10)
	if s := pageSlug(title); s != "" {
		return base + "/" + s
	}
	return base
}
