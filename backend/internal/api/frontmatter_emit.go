package api

import (
	"sort"
	"strings"

	"github.com/zcag/tela/backend/internal/mdimport"
	"github.com/zcag/tela/backend/internal/models"
	"gopkg.in/yaml.v3"
)

// emitPageFrontmatter renders a page as portable markdown: a canonical YAML
// frontmatter block followed by the body. This is the db → frontmatter-text
// direction of the page-properties contract (docs/page-properties.md).
//
// The system block is ALWAYS emitted. System keys are emit-only — synthesized
// from the source of truth (columns + pure derivations), never read back in — in
// a fixed order; the free-form props bag is spliced after them with keys sorted.
// space/parent/position are intentionally NOT emitted. The bag is re-filtered so
// a stray reserved key can never duplicate or override a system field.
func emitPageFrontmatter(p models.Page) string {
	root := &yaml.Node{Kind: yaml.MappingNode}
	add := func(key string, val any) {
		kn := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
		vn := &yaml.Node{}
		_ = vn.Encode(val)
		root.Content = append(root.Content, kn, vn)
	}

	// System keys (emit-only), fixed order.
	add("id", p.ID)
	add("title", p.Title)
	if slug := pageSlug(p.Title); slug != "" {
		add("slug", slug)
	}
	add("link", mcpPageURL(p))
	if p.CreatedAt != "" {
		add("created", p.CreatedAt)
	}
	if p.UpdatedAt != "" {
		add("updated", p.UpdatedAt)
	}

	// Free-form bag, keys sorted; reserved keys defensively dropped (props are
	// already filtered on ingress, but never let one collide with a system key).
	bag := make(map[string]any, len(p.Props))
	for k, v := range p.Props {
		bag[k] = v
	}
	mdimport.FilterReserved(bag)
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
	return sb.String()
}
