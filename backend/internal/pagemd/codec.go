// Package pagemd is the pure, dependency-light codec between a tela page and its
// canonical markdown representation (YAML frontmatter + body). It is the single
// home for the round-trip pair — Decode (text→data) and Encode (data→text) — so
// the HTTP API, the import pipeline, and future consumers (file-sync, a WebDAV
// backend) all share one mechanism.
//
// Design rules that keep it foundation-grade:
//   - No I/O, no globals, no config reads. Encode takes baseURL as a parameter;
//     given the same inputs it produces byte-identical output (so a sync layer
//     can hash/diff results trivially).
//   - Imports only the leaf models package + yaml. Never the api/HTTP layer.
//   - System/reserved frontmatter keys (id/title/slug/link/created/…) are owned
//     by tela and are emit-only: dropped on Decode, synthesized on Encode.
package pagemd

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/zcag/tela/backend/internal/models"
	"gopkg.in/yaml.v3"
)

// frontmatterRE matches a leading YAML-frontmatter block (LF or CRLF). Group 1
// captures the inner YAML.
var frontmatterRE = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n---\r?\n?`)

// reservedKeys are frontmatter keys tela owns via a column or a pure derivation.
// They never live in the props bag: dropped on Decode, synthesized on Encode.
// Matched case-insensitively.
var reservedKeys = map[string]bool{
	"id": true, "title": true, "slug": true, "link": true, "url": true,
	"created": true, "date": true, "updated": true, "modified": true,
	"position": true, "parent": true, "space": true,
}

// ---- Decode (text → data) -------------------------------------------------

// Decode splits canonical markdown into its body, frontmatter title, and the
// free-form props bag (reserved keys dropped, JSON-safe, nil when there is no
// frontmatter). A leading `---…---` block is treated as frontmatter only when
// its inner text parses to a YAML mapping; a scalar/sequence block (e.g. a
// markdown thematic break) is left untouched so it can never crash or be eaten.
func Decode(content string) (body, title string, props map[string]any) {
	loc := frontmatterRE.FindStringSubmatchIndex(content)
	if loc == nil {
		return content, "", nil
	}
	inner := content[loc[2]:loc[3]]

	var m map[string]any
	if err := yaml.Unmarshal([]byte(inner), &m); err != nil {
		return content, "", nil
	}
	body = content[loc[1]:]
	if m == nil {
		m = map[string]any{}
	}
	m = jsonSafeMap(m)

	for k, v := range m {
		if strings.ToLower(k) == "title" && v != nil {
			title = strings.TrimSpace(fmt.Sprintf("%v", v))
			break
		}
	}
	return body, title, FilterReserved(m)
}

// FilterReserved removes reserved keys from a props bag, mutating and returning
// it. Apply at every props ingress (frontmatter parse AND an explicit props
// field) so the drop rule holds regardless of path.
func FilterReserved(props map[string]any) map[string]any {
	for k := range props {
		if reservedKeys[strings.ToLower(k)] {
			delete(props, k)
		}
	}
	return props
}

// jsonSafeMap recursively coerces a parsed-YAML map so it is safe to marshal
// into JSON/JSONB: nested maps with non-string keys are rebuilt with stringified
// keys. yaml timestamps stay time.Time and serialize to RFC3339 (an accepted,
// documented normalization — value-faithful, not byte-faithful).
func jsonSafeMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = jsonSafe(v)
	}
	return out
}

func jsonSafe(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return jsonSafeMap(t)
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[fmt.Sprintf("%v", k)] = jsonSafe(vv)
		}
		return out
	case []any:
		for i := range t {
			t[i] = jsonSafe(t[i])
		}
		return t
	default:
		return v
	}
}

// ---- Encode (data → text) -------------------------------------------------

// Encode renders a page as canonical markdown: a YAML frontmatter block followed
// by the body. The system block is always emitted. System keys are synthesized
// in a fixed order (baseURL is injected — no global reads); the props bag is
// spliced after, keys sorted, and re-filtered so a stray reserved key can never
// collide with a system field. Deterministic for a given (page, baseURL).
func Encode(p models.Page, baseURL string) []byte {
	root := &yaml.Node{Kind: yaml.MappingNode}
	add := func(key string, val any) {
		kn := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
		vn := &yaml.Node{}
		_ = vn.Encode(val)
		root.Content = append(root.Content, kn, vn)
	}

	slug := Slug(p.Title)
	add("id", p.ID)
	add("title", p.Title)
	if slug != "" {
		add("slug", slug)
	}
	add("link", pageLink(baseURL, p.SpaceID, p.ID, slug))
	if p.CreatedAt != "" {
		add("created", p.CreatedAt)
	}
	if p.UpdatedAt != "" {
		add("updated", p.UpdatedAt)
	}

	bag := make(map[string]any, len(p.Props))
	for k, v := range p.Props {
		bag[k] = v
	}
	FilterReserved(bag)
	keys := make([]string, 0, len(bag))
	for k := range bag {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		add(k, bag[k])
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	if out, err := yaml.Marshal(root); err == nil {
		sb.Write(out)
	}
	sb.WriteString("---\n")
	sb.WriteString(p.Body)
	return []byte(sb.String())
}

// pageLink builds the page's canonical app URL from an injected base. Mirrors
// api.pageAppPath; the slug suffix is omitted when the title yields none.
func pageLink(baseURL string, spaceID, id int64, slug string) string {
	p := baseURL + "/spaces/" + strconv.FormatInt(spaceID, 10) + "/pages/" + strconv.FormatInt(id, 10)
	if slug != "" {
		p += "/" + slug
	}
	return p
}

// ---- Slug (pure, title → url-safe slug) -----------------------------------

const maxSlugLen = 60

// slugTranslit maps the accented letters tela actually sees (Turkish + common
// Latin diacritics) to ASCII. Anything else not [a-z0-9] is dropped.
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

// Slug derives a URL-safe, lowercase, hyphen-joined slug from a title.
// Truncates at a word boundary to <= maxSlugLen, "" when nothing usable remains.
// Mirrored in the frontend (src/lib/slug.ts) — keep the two in sync.
func Slug(title string) string {
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
