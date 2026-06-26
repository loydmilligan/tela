package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/zcag/tela/backend/internal/atlas/core"
)

const (
	repairThreshold = 0.95 // must-cover rate at which we stop
	repairMaxIter   = 3
)

// repairStage closes coverage gaps. While the must-cover surface (routes,
// entrypoints, flags, env, models) isn't fully documented, it regenerates the
// reference page responsible for the missing kind, explicitly listing what was
// dropped — then re-audits. Bounded iterations keep it from looping forever.
type repairStage struct{}

func (repairStage) Name() core.StageName { return core.StageRepair }

func (repairStage) Run(ctx context.Context, rc *RunContext) error {
	if err := repairMustCover(ctx, rc); err != nil {
		return err
	}
	if err := repairMermaid(ctx, rc); err != nil {
		return err
	}
	rc.Coverage = computeCoverage(rc)
	_ = rc.Store.SaveRunCoverage(rc.Run.ID, rc.Coverage)
	if g := mustCoverGaps(rc.Coverage.Gaps); len(g) > 0 {
		rc.Warn("residual gaps after repair: %s", gapSample(g, 12))
	}
	if rc.Coverage.MermaidInvalid > 0 {
		rc.Warn("residual mermaid issues after repair: %d invalid", rc.Coverage.MermaidInvalid)
	}
	return nil
}

// repairMustCover closes must-cover surface gaps by re-drafting the responsible
// reference pages, explicitly listing the dropped items. Bounded iterations.
func repairMustCover(ctx context.Context, rc *RunContext) error {
	for iter := 1; iter <= repairMaxIter; iter++ {
		rc.Coverage = computeCoverage(rc)
		if rc.Coverage.MustRate() >= repairThreshold {
			rc.Info("coverage sufficient: must-cover %.0f%%", 100*rc.Coverage.MustRate())
			return nil
		}
		missing := mustCoverGaps(rc.Coverage.Gaps)
		if len(missing) == 0 {
			return nil
		}
		rc.Step(iter, repairMaxIter, "repair pass %d: %d gap(s) in must-cover surface", iter, len(missing))

		// Collect the reference pages responsible for a missing kind, then
		// re-draft them concurrently (the outer iteration stays sequential — it
		// re-audits coverage between passes). Each worker writes only its own page.
		type target struct {
			i    int
			miss []core.Gap
		}
		var targets []target
		for i := range rc.Art.Pages {
			p := &rc.Art.Pages[i]
			if p.Kind != core.PageReference {
				continue
			}
			if miss := gapsForKinds(missing, p.SpineKinds); len(miss) > 0 {
				targets = append(targets, target{i, miss})
			}
		}
		if len(targets) == 0 {
			break // nothing actionable (gaps in kinds without a reference page)
		}
		err := parallelN(ctx, pageFanout, len(targets), func(ctx context.Context, t int) error {
			p := &rc.Art.Pages[targets[t].i]
			body, err := redraftReference(ctx, rc, p, targets[t].miss)
			if err != nil {
				return err
			}
			p.Body = body
			return rc.Store.UpdatePageBody(p.ID, body)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

const mermaidRepairMaxIter = 2 // bound on the mermaid re-draft loop

// repairMermaid re-drafts pages whose mermaid blocks failed the structural check,
// instructing a valid, simple diagram. Bounded (≤2 iters); stops when no gaps.
func repairMermaid(ctx context.Context, rc *RunContext) error {
	for iter := 1; iter <= mermaidRepairMaxIter; iter++ {
		rc.Coverage = computeCoverage(rc)
		gaps := rc.Coverage.MermaidGaps
		if len(gaps) == 0 {
			return nil
		}
		// dedupe pages with at least one bad mermaid block
		badPages := map[string]bool{}
		for _, g := range gaps {
			badPages[g.Page] = true
		}
		rc.Step(iter, mermaidRepairMaxIter, "mermaid repair pass %d: %d invalid block(s) across %d page(s)", iter, len(gaps), len(badPages))

		// Re-draft each page with a bad mermaid block concurrently (outer loop
		// stays sequential — it re-audits between passes). Each worker writes its
		// own page.
		var targets []int
		for i := range rc.Art.Pages {
			if badPages[rc.Art.Pages[i].Slug] {
				targets = append(targets, i)
			}
		}
		if len(targets) == 0 {
			break
		}
		err := parallelN(ctx, pageFanout, len(targets), func(ctx context.Context, t int) error {
			p := &rc.Art.Pages[targets[t]]
			body, err := redraftMermaid(ctx, rc, p)
			if err != nil {
				return err
			}
			p.Body = body
			return rc.Store.UpdatePageBody(p.ID, body)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// mermaidRepairSystem / mermaidRepairUser keep the repair prompt local to this
// file (prompts.go is owned by another change). The instruction forces a valid,
// simple diagram so the cheap structural check passes on the next audit.
const mermaidRepairSystem = `You are fixing broken Mermaid diagrams in an existing technical documentation page.
Return the COMPLETE page markdown, unchanged except that every ` + "```mermaid```" + ` block is a VALID, SIMPLE Mermaid diagram.
Rules for each diagram:
- Start with one diagram-type header (graph, flowchart, sequenceDiagram, classDiagram, stateDiagram, erDiagram, gantt, journey, pie, mindmap, or timeline).
- Use exactly one diagram type per block.
- ALWAYS quote every node label, e.g. A["Schema: Types"] — never leave a label unquoted.
- An UNQUOTED label MUST NOT contain ':' '(' ')' or any non-ASCII character (e.g. ü, ç, ş, İ) — mermaid's parser breaks on them. Put any such label in double quotes.
- Keep brackets ([] () {}) balanced.
Do not add commentary, headings, or code fences around your answer — output only the page markdown.`

// redraftMermaid asks the model to rewrite a page, repairing its mermaid blocks.
func redraftMermaid(ctx context.Context, rc *RunContext, p *core.Page) (string, error) {
	user := "PAGE TITLE: " + p.Title + "\n\nREWRITE THIS PAGE, fixing only its Mermaid diagrams per the rules:\n\n" + p.Body
	body, err := rc.LLM.Chat(ctx, mermaidRepairSystem, user, 0.2)
	return sanitizePage(body), err
}

// redraftReference regenerates a reference page, forcing the previously-missing
// items to appear.
func redraftReference(ctx context.Context, rc *RunContext, p *core.Page, miss []core.Gap) (string, error) {
	items := rc.Art.SpineByKind(p.SpineKinds...)
	var list strings.Builder
	q := make([]string, 0, len(items))
	for _, it := range items {
		fmt.Fprintf(&list, "- [%s] %s  (%s:%d)%s\n", it.Kind, it.Name, it.File, it.Line, detailSuffix(it.Detail))
		q = append(q, it.Name)
	}
	var emph strings.Builder
	emph.WriteString("CRITICAL: the previous draft OMITTED these items. They MUST each appear in your output:\n")
	for _, g := range miss {
		fmt.Fprintf(&emph, "- %s (%s:%d)\n", g.Name, g.File, g.Line)
	}
	chunks, err := retrieve(ctx, rc, strings.Join(q, " "), retrieveK)
	if err != nil {
		return "", err
	}
	prompt := emph.String() + "\n" + refUser(p.Title, list.String(), assembleContext(chunks))
	body, err := rc.LLM.Chat(ctx, refSystem, prompt, 0.2)
	return sanitizePage(body), err
}

func mustCoverGaps(gaps []core.Gap) []core.Gap {
	var out []core.Gap
	for _, g := range gaps {
		if core.MustCoverKinds[g.Kind] {
			out = append(out, g)
		}
	}
	return out
}

func gapsForKinds(gaps []core.Gap, kinds []core.SpineKind) []core.Gap {
	want := map[core.SpineKind]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	var out []core.Gap
	for _, g := range gaps {
		if want[g.Kind] {
			out = append(out, g)
		}
	}
	return out
}
