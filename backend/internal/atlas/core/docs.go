package core

// PageKind distinguishes LLM-planned narrative pages from deterministic
// reference pages whose content is anchored to the spine (guaranteed coverage).
type PageKind string

const (
	PageNarrative PageKind = "narrative" // LLM-designed topic page
	PageReference PageKind = "reference" // spine-anchored: routes, config, etc.
)

// Gap is a surface item the generated docs failed to mention.
type Gap struct {
	Kind SpineKind `json:"kind"`
	Name string    `json:"name"`
	File string    `json:"file"`
	Line int       `json:"line"`
}

// MermaidGap is one mermaid block that failed the cheap structural check, with
// the page it lives on and a short reason — what the repair loop acts on.
type MermaidGap struct {
	Page string `json:"page"` // page slug
	Err  string `json:"err"`  // why it's flagged (empty / missing header / unbalanced)
}

// Coverage is the objective audit of the generated docs against the spine — the
// measurement DeepWiki can't do because it has no ground-truth surface.
type Coverage struct {
	Total        int   `json:"total"`         // all surface items
	Covered      int   `json:"covered"`       // mentioned somewhere in the docs
	MustTotal    int   `json:"must_total"`    // items of must-cover kinds
	MustCovered  int   `json:"must_covered"`  // of those, covered
	Gaps         []Gap `json:"gaps"`          // uncovered items (all kinds)
	Citations    int   `json:"citations"`     // file:line citations found
	BadCitations int   `json:"bad_citations"` // citations that don't resolve
	// BadCites is a deduped, capped sample of the unresolved `path:line` strings,
	// surfaced so the overview/UI can list what failed to resolve.
	BadCites []string `json:"bad_cites,omitempty"`
	Mermaid  int      `json:"mermaid"` // mermaid diagrams found
	// Mermaid structural validation (cheap heuristic; see validateMermaid).
	MermaidValid   int          `json:"mermaid_valid"`
	MermaidInvalid int          `json:"mermaid_invalid"`
	MermaidGaps    []MermaidGap `json:"mermaid_gaps,omitempty"`
}

func (c Coverage) Rate() float64 {
	if c.Total == 0 {
		return 1
	}
	return float64(c.Covered) / float64(c.Total)
}

// MustRate is coverage over the interface surface that completeness questions
// actually probe — the number that matters most.
func (c Coverage) MustRate() float64 {
	if c.MustTotal == 0 {
		return 1
	}
	return float64(c.MustCovered) / float64(c.MustTotal)
}

// MustCoverKinds are the surface kinds a complete doc must enumerate.
var MustCoverKinds = map[SpineKind]bool{
	KindEntrypoint: true, KindRoute: true, KindFlag: true, KindEnv: true, KindDBModel: true,
}

// Page is one document in the generated wiki.
type Page struct {
	ID         int64       `json:"id"`
	Order      int         `json:"order"`
	Kind       PageKind    `json:"kind"`
	Title      string      `json:"title"`
	Slug       string      `json:"slug"`
	Summary    string      `json:"summary"`
	Topics     []string    `json:"topics,omitempty"`
	SpineKinds []SpineKind `json:"-"`
	Body       string      `json:"body,omitempty"`
}
