// Command atlasdiff is the fidelity gate for the atlas → in-tela doc-generation
// port: it diffs two tela documentation spaces (a REFERENCE space produced by the
// original "atlas" system against a NEW space produced by the in-tela port) and
// reports any material divergence, exiting non-zero when the port regressed.
//
// Both spaces are ordinary tela spaces whose root page carries an atlas-rendered
// "coverage overview" (see atlas internal/engine/deliver.go renderOverview) and
// whose child pages are the generated docs. Because both systems render the
// overview with the SAME code, its tables are directly comparable.
//
// REST surface (verified against backend/internal/api/router.go + pages.go):
//   - GET /api/pages?space_id={id}&tree=1  -> {"pages":[node,...]} the full nested
//     page tree, each node embedding models.Page (id, parent_id, title, body,
//     position, props) plus a "children" slice already ordered by position. One
//     call yields title+parent+order+body+props for the whole space, so we never
//     need GET /api/pages/{id}.
//
// Both reads carry "Authorization: Bearer <PAT>". The two spaces may live on
// different tela instances, hence a separate base URL + token per side.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Page tree model
// ---------------------------------------------------------------------------

// node mirrors the backend pageNode JSON: models.Page fields plus children.
type node struct {
	ID       int64          `json:"id"`
	ParentID *int64         `json:"parent_id"`
	Title    string         `json:"title"`
	Body     string         `json:"body"`
	Position int64          `json:"position"`
	Props    map[string]any `json:"props"`
	Children []*node        `json:"children"`
}

type treeResp struct {
	Pages []*node `json:"pages"`
}

// page is a flattened node with its parent title resolved, for diffing.
type page struct {
	title       string // normalized
	rawTitle    string // original, for display
	parentTitle string // normalized parent title, "" at root
	body        string
	props       map[string]any
	childTitles []string // normalized, in position order
	isRoot      bool     // body holds an atlas "## Coverage" overview
}

// ---------------------------------------------------------------------------
// Fetch
// ---------------------------------------------------------------------------

func fetchTree(baseURL, token string, spaceID int) ([]*node, error) {
	u := strings.TrimRight(baseURL, "/") + "/api/pages?space_id=" + strconv.Itoa(spaceID) + "&tree=1"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s: %s", u, resp.Status, strings.TrimSpace(string(body)))
	}
	var tr treeResp
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("decode tree: %w", err)
	}
	return tr.Pages, nil
}

// flatten walks the nested tree into a title-keyed map. Children order is taken
// from the nested slices (already position-sorted by the backend).
func flatten(roots []*node) map[string]*page {
	out := map[string]*page{}
	var walk func(n *node, parent string)
	walk = func(n *node, parent string) {
		nt := normTitle(n.Title)
		p := &page{
			title:       nt,
			rawTitle:    n.Title,
			parentTitle: parent,
			body:        n.Body,
			props:       n.Props,
			isRoot:      hasCoverageOverview(n.Body),
		}
		for _, c := range n.Children {
			p.childTitles = append(p.childTitles, normTitle(c.Title))
		}
		out[nt] = p
		for _, c := range n.Children {
			walk(c, nt)
		}
	}
	for _, r := range roots {
		walk(r, "")
	}
	return out
}

// ---------------------------------------------------------------------------
// Overview / coverage parsing
// ---------------------------------------------------------------------------

// coverage is the parsed atlas "## Coverage" overview of a single source root.
type coverage struct {
	Covered, Total       int
	CoveredPct           float64
	MustCovered, MustTot int
	MustPct              float64
	Citations, Unresolv  int
	Diagrams             int
	Inventory            map[string]int // kind -> count
	Gaps                 []string       // raw gap bullet text
}

func hasCoverageOverview(body string) bool {
	return strings.Contains(body, "## Coverage")
}

var (
	// "40/45 (89%)" -> covered, total, pct
	reNM = regexp.MustCompile(`(\d+)\s*/\s*(\d+)\s*\((\d+)%\)`)
	// "30 (1 unresolved)" -> citations, unresolved
	reCite = regexp.MustCompile(`(\d+)\s*\((\d+)\s+unresolved\)`)
)

// parseOverview extracts the coverage table, surface inventory and gap list from
// an atlas overview page body. Mirrors renderOverview's exact layout:
//
//	## Coverage
//	| Surface covered | Must-cover | Citations | Diagrams |
//	|---|---|---|---|
//	| 40/45 (89%) | 22/24 (92%) | 30 (1 unresolved) | 5 |
//	### Undocumented surface (N)
//	- `kind` name — `file:line`
//	## Surface inventory
//	| Kind | Count |
//	|---|---|
//	| func | 30 |
func parseOverview(body string) coverage {
	c := coverage{Inventory: map[string]int{}}
	lines := strings.Split(body, "\n")

	section := ""
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		switch {
		case strings.HasPrefix(line, "## Coverage"):
			section = "coverage"
			continue
		case strings.HasPrefix(line, "### Undocumented surface"):
			section = "gaps"
			continue
		case strings.HasPrefix(line, "## Surface inventory"):
			section = "inventory"
			continue
		case strings.HasPrefix(line, "## "):
			section = ""
			continue
		}

		switch section {
		case "coverage":
			// The data row is the table line carrying the n/m (%) cells.
			if strings.HasPrefix(line, "|") && reNM.MatchString(line) {
				cells := splitRow(line)
				if len(cells) >= 4 {
					if m := reNM.FindStringSubmatch(cells[0]); m != nil {
						c.Covered, c.Total = atoi(m[1]), atoi(m[2])
						c.CoveredPct = float64(atoi(m[3]))
					}
					if m := reNM.FindStringSubmatch(cells[1]); m != nil {
						c.MustCovered, c.MustTot = atoi(m[1]), atoi(m[2])
						c.MustPct = float64(atoi(m[3]))
					}
					if m := reCite.FindStringSubmatch(cells[2]); m != nil {
						c.Citations, c.Unresolv = atoi(m[1]), atoi(m[2])
					} else {
						c.Citations = firstInt(cells[2])
					}
					c.Diagrams = firstInt(cells[3])
				}
			}
		case "gaps":
			if strings.HasPrefix(line, "- ") {
				c.Gaps = append(c.Gaps, strings.TrimSpace(line[2:]))
			}
		case "inventory":
			// Skip header + separator; collect "| kind | count |" rows.
			if strings.HasPrefix(line, "|") && !strings.Contains(line, "---") {
				cells := splitRow(line)
				if len(cells) >= 2 {
					kind := strings.TrimSpace(cells[0])
					if kind == "" || strings.EqualFold(kind, "Kind") {
						continue
					}
					if n, err := strconv.Atoi(strings.TrimSpace(cells[1])); err == nil {
						c.Inventory[kind] = n
					}
				}
			}
		}
	}
	return c
}

// splitRow splits a markdown table row "| a | b | c |" into trimmed cells.
func splitRow(line string) []string {
	line = strings.Trim(strings.TrimSpace(line), "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// ---------------------------------------------------------------------------
// Body similarity
// ---------------------------------------------------------------------------

var reWord = regexp.MustCompile(`[a-z0-9]+`)

// tokenJaccard is the Jaccard similarity of the lowercased word-token sets of two
// bodies — a coarse content-drift signal robust to wording/whitespace/run noise.
func tokenJaccard(a, b string) float64 {
	sa, sb := tokenSet(a), tokenSet(b)
	if len(sa) == 0 && len(sb) == 0 {
		return 1
	}
	inter := 0
	for t := range sa {
		if _, ok := sb[t]; ok {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	if union == 0 {
		return 1
	}
	return float64(inter) / float64(union)
}

func tokenSet(s string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, w := range reWord.FindAllString(strings.ToLower(s), -1) {
		set[w] = struct{}{}
	}
	return set
}

func lineCount(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Diff
// ---------------------------------------------------------------------------

type bodyDrift struct {
	Title    string  `json:"title"`
	Jaccard  float64 `json:"jaccard"`
	RefLines int     `json:"ref_lines"`
	NewLines int     `json:"new_lines"`
}

type covDelta struct {
	Root         string   `json:"root"`
	CoveredPctΔ  float64  `json:"covered_pct_delta"`
	MustPctΔ     float64  `json:"must_pct_delta"`
	RefCovered   string   `json:"ref_covered"`
	NewCovered   string   `json:"new_covered"`
	RefMust      string   `json:"ref_must"`
	NewMust      string   `json:"new_must"`
	CitationsΔ   int      `json:"citations_delta"`
	UnresolvedΔ  int      `json:"unresolved_delta"`
	DiagramsΔ    int      `json:"diagrams_delta"`
	GapsOnlyRef  []string `json:"gaps_only_ref"`
	GapsOnlyNew  []string `json:"gaps_only_new"`
	InvOnlyRef   []string `json:"inventory_only_ref"`
	InvOnlyNew   []string `json:"inventory_only_new"`
	InvCountDiff []string `json:"inventory_count_diff"`
	Material     bool     `json:"material"`
}

type report struct {
	RefCount       int         `json:"ref_count"`
	NewCount       int         `json:"new_count"`
	MissingInNew   []string    `json:"missing_in_new"`  // in ref, absent in new
	ExtraInNew     []string    `json:"extra_in_new"`    // in new, absent in ref
	StructureDiffs []string    `json:"structure_diffs"` // parent/order mismatches
	Coverage       []covDelta  `json:"coverage"`
	BodyDrift      []bodyDrift `json:"body_drift"` // matched pages below threshold
	PageSetOK      bool        `json:"page_set_ok"`
	StructureOK    bool        `json:"structure_ok"`
	CoverageOK     bool        `json:"coverage_ok"`
	Material       bool        `json:"material"` // overall: any page-set/structure/coverage regression
}

func diff(ref, neu map[string]*page, threshold, covTol float64) report {
	var r report
	r.RefCount, r.NewCount = len(ref), len(neu)

	// --- Page set (matched by normalized title) ---
	for t, p := range ref {
		if _, ok := neu[t]; !ok {
			r.MissingInNew = append(r.MissingInNew, p.rawTitle)
		}
	}
	for t, p := range neu {
		if _, ok := ref[t]; !ok {
			r.ExtraInNew = append(r.ExtraInNew, p.rawTitle)
		}
	}
	sort.Strings(r.MissingInNew)
	sort.Strings(r.ExtraInNew)
	r.PageSetOK = len(r.MissingInNew) == 0 && len(r.ExtraInNew) == 0

	// --- Tree structure: for each matched page compare parent + child order ---
	var matched []string
	for t := range ref {
		if _, ok := neu[t]; ok {
			matched = append(matched, t)
		}
	}
	sort.Strings(matched)
	for _, t := range matched {
		rp, np := ref[t], neu[t]
		if rp.parentTitle != np.parentTitle {
			r.StructureDiffs = append(r.StructureDiffs, fmt.Sprintf(
				"%q parent: ref=%s new=%s", rp.rawTitle,
				orRoot(rp.parentTitle), orRoot(np.parentTitle)))
		}
		// Compare child order over the intersection of titles both sides know,
		// so a missing/extra page (already reported) doesn't double-count here.
		rc := intersectSeq(rp.childTitles, np.childTitles)
		nc := intersectSeq(np.childTitles, rp.childTitles)
		if !eqSeq(rc, nc) {
			r.StructureDiffs = append(r.StructureDiffs, fmt.Sprintf(
				"%q child order differs: ref=[%s] new=[%s]", rp.rawTitle,
				strings.Join(rc, ", "), strings.Join(nc, ", ")))
		}
	}
	r.StructureOK = len(r.StructureDiffs) == 0

	// --- Coverage: match root overview pages by title, compare parsed tables ---
	r.CoverageOK = true
	for _, t := range matched {
		rp, np := ref[t], neu[t]
		if !rp.isRoot || !np.isRoot {
			continue
		}
		d := diffCoverage(rp.rawTitle, parseOverview(rp.body), parseOverview(np.body), covTol)
		r.Coverage = append(r.Coverage, d)
		if d.Material {
			r.CoverageOK = false
		}
	}

	// --- Body content drift (informational; does not flip exit by itself) ---
	for _, t := range matched {
		rp, np := ref[t], neu[t]
		j := tokenJaccard(rp.body, np.body)
		if j < threshold {
			r.BodyDrift = append(r.BodyDrift, bodyDrift{
				Title: rp.rawTitle, Jaccard: j,
				RefLines: lineCount(rp.body), NewLines: lineCount(np.body),
			})
		}
	}
	sort.Slice(r.BodyDrift, func(i, j int) bool { return r.BodyDrift[i].Jaccard < r.BodyDrift[j].Jaccard })

	r.Material = !r.PageSetOK || !r.StructureOK || !r.CoverageOK
	return r
}

func diffCoverage(root string, a, b coverage, tol float64) covDelta {
	d := covDelta{
		Root:        root,
		CoveredPctΔ: b.CoveredPct - a.CoveredPct,
		MustPctΔ:    b.MustPct - a.MustPct,
		RefCovered:  fmt.Sprintf("%d/%d (%.0f%%)", a.Covered, a.Total, a.CoveredPct),
		NewCovered:  fmt.Sprintf("%d/%d (%.0f%%)", b.Covered, b.Total, b.CoveredPct),
		RefMust:     fmt.Sprintf("%d/%d (%.0f%%)", a.MustCovered, a.MustTot, a.MustPct),
		NewMust:     fmt.Sprintf("%d/%d (%.0f%%)", b.MustCovered, b.MustTot, b.MustPct),
		CitationsΔ:  b.Citations - a.Citations,
		UnresolvedΔ: b.Unresolv - a.Unresolv,
		DiagramsΔ:   b.Diagrams - a.Diagrams,
	}
	d.GapsOnlyRef, d.GapsOnlyNew = setDiff(a.Gaps, b.Gaps)
	d.InvOnlyRef, d.InvOnlyNew, d.InvCountDiff = invDiff(a.Inventory, b.Inventory)

	// Material if a coverage metric regressed beyond tolerance, citations dropped
	// or unresolved grew, or the surface inventory / gap set diverged at all.
	d.Material = abs(d.CoveredPctΔ) > tol || abs(d.MustPctΔ) > tol ||
		d.UnresolvedΔ > 0 || d.CitationsΔ < 0 ||
		len(d.GapsOnlyRef) > 0 || len(d.GapsOnlyNew) > 0 ||
		len(d.InvOnlyRef) > 0 || len(d.InvOnlyNew) > 0 || len(d.InvCountDiff) > 0
	return d
}

// ---------------------------------------------------------------------------
// Reporting
// ---------------------------------------------------------------------------

func printReport(r report) {
	mark := func(ok bool) string {
		if ok {
			return "✓"
		}
		return "✗"
	}
	p := func(format string, a ...any) { fmt.Printf(format+"\n", a...) }

	p("atlasdiff — fidelity report")
	p("===========================")
	p("")

	p("%s Page set  (ref=%d, new=%d)", mark(r.PageSetOK), r.RefCount, r.NewCount)
	if len(r.MissingInNew) > 0 {
		p("    missing in new (%d):", len(r.MissingInNew))
		for _, t := range r.MissingInNew {
			p("      - %s", t)
		}
	}
	if len(r.ExtraInNew) > 0 {
		p("    extra in new (%d):", len(r.ExtraInNew))
		for _, t := range r.ExtraInNew {
			p("      + %s", t)
		}
	}
	p("")

	p("%s Tree structure", mark(r.StructureOK))
	for _, d := range r.StructureDiffs {
		p("    - %s", d)
	}
	p("")

	p("%s Coverage", mark(r.CoverageOK))
	for _, d := range r.Coverage {
		p("    %s %s", mark(!d.Material), d.Root)
		p("        surface-covered: ref %s  new %s  (Δ %+.0f%%)", d.RefCovered, d.NewCovered, d.CoveredPctΔ)
		p("        must-cover:      ref %s  new %s  (Δ %+.0f%%)", d.RefMust, d.NewMust, d.MustPctΔ)
		p("        citations Δ %+d  unresolved Δ %+d  diagrams Δ %+d", d.CitationsΔ, d.UnresolvedΔ, d.DiagramsΔ)
		for _, g := range d.GapsOnlyRef {
			p("        gap only in ref: %s", g)
		}
		for _, g := range d.GapsOnlyNew {
			p("        gap only in new: %s", g)
		}
		for _, k := range d.InvOnlyRef {
			p("        inventory kind only in ref: %s", k)
		}
		for _, k := range d.InvOnlyNew {
			p("        inventory kind only in new: %s", k)
		}
		for _, k := range d.InvCountDiff {
			p("        inventory count differs: %s", k)
		}
	}
	p("")

	bodyOK := len(r.BodyDrift) == 0
	p("%s Body content (Jaccard threshold; informational)", mark(bodyOK))
	for _, b := range r.BodyDrift {
		p("    - %s  jaccard=%.2f  lines ref=%d new=%d", b.Title, b.Jaccard, b.RefLines, b.NewLines)
	}
	p("")

	if r.Material {
		p("RESULT: ✗ material divergence — port is NOT faithful to reference")
	} else {
		p("RESULT: ✓ no material divergence")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func normTitle(t string) string { return strings.ToLower(strings.Join(strings.Fields(t), " ")) }

func orRoot(s string) string {
	if s == "" {
		return "(root)"
	}
	return s
}

func atoi(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }

func firstInt(s string) int {
	if m := regexp.MustCompile(`\d+`).FindString(s); m != "" {
		return atoi(m)
	}
	return 0
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// intersectSeq returns a's items that also appear in b, preserving a's order.
func intersectSeq(a, b []string) []string {
	set := map[string]struct{}{}
	for _, x := range b {
		set[x] = struct{}{}
	}
	var out []string
	for _, x := range a {
		if _, ok := set[x]; ok {
			out = append(out, x)
		}
	}
	return out
}

func eqSeq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// setDiff returns (onlyA, onlyB) for two string slices treated as sets.
func setDiff(a, b []string) (onlyA, onlyB []string) {
	sa, sb := toSet(a), toSet(b)
	for x := range sa {
		if _, ok := sb[x]; !ok {
			onlyA = append(onlyA, x)
		}
	}
	for x := range sb {
		if _, ok := sa[x]; !ok {
			onlyB = append(onlyB, x)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	return
}

func invDiff(a, b map[string]int) (onlyA, onlyB, countDiff []string) {
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			onlyA = append(onlyA, fmt.Sprintf("%s (%d)", k, av))
		} else if av != bv {
			countDiff = append(countDiff, fmt.Sprintf("%s: ref=%d new=%d", k, av, bv))
		}
	}
	for k, bv := range b {
		if _, ok := a[k]; !ok {
			onlyB = append(onlyB, fmt.Sprintf("%s (%d)", k, bv))
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	sort.Strings(countDiff)
	return
}

func toSet(s []string) map[string]struct{} {
	m := map[string]struct{}{}
	for _, x := range s {
		m[x] = struct{}{}
	}
	return m
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	var (
		refURL    = flag.String("ref-url", "", "reference instance base URL (e.g. https://tela.cagdas.io)")
		refToken  = flag.String("ref-token", "", "reference instance PAT (bearer)")
		refSpace  = flag.Int("ref-space", 0, "reference space id")
		newURL    = flag.String("new-url", "", "new instance base URL")
		newToken  = flag.String("new-token", "", "new instance PAT (bearer)")
		newSpace  = flag.Int("new-space", 0, "new space id")
		threshold = flag.Float64("threshold", 0.6, "body-similarity Jaccard threshold; matched pages below it are flagged")
		covTol    = flag.Float64("cov-tolerance", 2.0, "coverage percentage-point tolerance before a delta is material")
		asJSON    = flag.Bool("json", false, "also emit a machine-readable JSON summary")
	)
	flag.Parse()

	missing := []string{}
	for name, v := range map[string]string{"ref-url": *refURL, "ref-token": *refToken, "new-url": *newURL, "new-token": *newToken} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if *refSpace <= 0 {
		missing = append(missing, "ref-space")
	}
	if *newSpace <= 0 {
		missing = append(missing, "new-space")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		fmt.Fprintf(os.Stderr, "missing required flags: %s\n\n", strings.Join(missing, ", "))
		flag.Usage()
		os.Exit(2)
	}

	refRoots, err := fetchTree(*refURL, *refToken, *refSpace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch ref space: %v\n", err)
		os.Exit(2)
	}
	newRoots, err := fetchTree(*newURL, *newToken, *newSpace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch new space: %v\n", err)
		os.Exit(2)
	}

	r := diff(flatten(refRoots), flatten(newRoots), *threshold, *covTol)
	printReport(r)

	if *asJSON {
		fmt.Println("\n--- JSON ---")
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(r)
	}

	if r.Material {
		os.Exit(1)
	}
}
