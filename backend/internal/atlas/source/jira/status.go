package jira

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// status.go turns the fetched issue set into a tracker's *current state* — the
// thing a Jira project is actually about (how many issues sit in each status,
// how far each epic has progressed, what's blocked / in flight, what moved
// recently). renderStatus writes this as status.md alongside schema.md; the
// connector's Spine then reads the state surface back out of it (parseStatusSpine)
// so "current state is documented" becomes a must-cover coverage target.
//
// The file is both human-readable prose AND machine-parseable: each state item
// is emitted as a `- <Name> — <detail>` list line under a known `## ` heading,
// mirroring schema.md's contract so the spine stays a pure function of the
// acquired snapshot (no second network call).

// doneStems / blockedStems / inFlightStems classify status names into terminal,
// blocked, and in-flight buckets by substring, case-insensitively. Jira projects
// name statuses freely ("Done", "Closed", "Resolved"; "Blocked", "On Hold",
// "Impeded"; "In Progress", "In Review", "QA"), so we match common stems rather
// than an exact set.
var doneStems = []string{"done", "closed", "resolved", "complete", "cancel", "won't"}
var blockedStems = []string{"block", "hold", "impede", "stuck", "wait"}
var inFlightStems = []string{"progress", "review", "qa", "test", "doing", "develop"}

func matchesAny(name string, stems []string) bool {
	n := strings.ToLower(name)
	for _, s := range stems {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
}

// statusState is the computed current-state model for a project's issue set.
type statusState struct {
	total     int
	counts    []statusCount // issues per status, descending then by name
	epics     []epicProgress
	blocked   []issueRef // issues in a blocked-looking status
	inFlight  []issueRef // issues in an in-progress-looking status
	recent7   int        // issues updated in the last 7 days
	recent30  int        // issues updated in the last 30 days
	created7  int        // issues created in the last 7 days
	created30 int        // issues created in the last 30 days
}

type statusCount struct {
	name string
	n    int
}

type issueRef struct {
	key, summary, status string
}

// epicProgress is one epic (parent) and the status roll-up of its children.
type epicProgress struct {
	key, summary string
	done, total  int
	statuses     []statusCount // child status breakdown
}

func (e epicProgress) pct() int {
	if e.total == 0 {
		return 0
	}
	return e.done * 100 / e.total
}

// computeStatus folds the issue set into the current-state model. `now` is
// injectable so recency is testable.
func computeStatus(issues []issue, now time.Time) statusState {
	st := statusState{total: len(issues)}

	// status distribution
	cnt := map[string]int{}
	for _, is := range issues {
		s := strings.TrimSpace(is.Fields.Status.Name)
		if s == "" {
			s = "(no status)"
		}
		cnt[s]++
		ref := issueRef{key: is.Key, summary: is.Fields.Summary, status: s}
		if matchesAny(s, blockedStems) {
			st.blocked = append(st.blocked, ref)
		} else if matchesAny(s, inFlightStems) {
			st.inFlight = append(st.inFlight, ref)
		}
	}
	st.counts = sortedCounts(cnt)

	// epic roll-up: group children by their parent key, where the parent's issue
	// type is Epic. We also need each epic's own summary, which we learn from
	// either the issue (if the epic itself is in the set) or its children's parent
	// ref.
	epicSummary := map[string]string{}
	children := map[string][]issue{}
	for _, is := range issues {
		if strings.EqualFold(is.Fields.IssueType.Name, "Epic") {
			epicSummary[is.Key] = is.Fields.Summary
		}
		if p := is.Fields.Parent; p != nil && strings.EqualFold(p.Fields.IssueType.Name, "Epic") {
			children[p.Key] = append(children[p.Key], is)
			if _, ok := epicSummary[p.Key]; !ok {
				epicSummary[p.Key] = p.Fields.Summary
			}
		}
	}
	keys := make([]string, 0, len(epicSummary))
	for k := range epicSummary {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		kids := children[k]
		ep := epicProgress{key: k, summary: epicSummary[k], total: len(kids)}
		sc := map[string]int{}
		for _, c := range kids {
			s := strings.TrimSpace(c.Fields.Status.Name)
			sc[s]++
			if matchesAny(s, doneStems) {
				ep.done++
			}
		}
		ep.statuses = sortedCounts(sc)
		st.epics = append(st.epics, ep)
	}

	// recency
	for _, is := range issues {
		if d, ok := daysSince(is.Fields.Updated, now); ok {
			if d <= 7 {
				st.recent7++
			}
			if d <= 30 {
				st.recent30++
			}
		}
		if d, ok := daysSince(is.Fields.Created, now); ok {
			if d <= 7 {
				st.created7++
			}
			if d <= 30 {
				st.created30++
			}
		}
	}

	sortRefs(st.blocked)
	sortRefs(st.inFlight)
	return st
}

func sortedCounts(m map[string]int) []statusCount {
	out := make([]statusCount, 0, len(m))
	for k, v := range m {
		out = append(out, statusCount{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].name < out[j].name
	})
	return out
}

func sortRefs(rs []issueRef) {
	sort.Slice(rs, func(i, j int) bool { return rs[i].key < rs[j].key })
}

// daysSince parses a Jira timestamp and returns whole days between it and now.
func daysSince(ts string, now time.Time) (int, bool) {
	r := normalizeRef(ts)
	t, err := time.Parse(time.RFC3339, r)
	if err != nil {
		return 0, false
	}
	d := now.Sub(t).Hours() / 24
	if d < 0 {
		d = 0
	}
	return int(d), true
}

// renderStatus writes status.md. The headings + `- Name — detail` list shape are
// the contract parseStatusSpine reads back. Sections: current state (counts per
// status), epics & progress, blocked / in-flight, recent changes.
func renderStatus(key string, st statusState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Jira Project %s — Current State\n\n", key)
	fmt.Fprintf(&b, "A snapshot of where this project stands right now: %d issue(s) total, "+
		"how they distribute across statuses, how far each epic has progressed, what is "+
		"blocked or in flight, and what changed recently.\n", st.total)

	b.WriteString("\n## Current state\n\n")
	if len(st.counts) == 0 {
		b.WriteString("—\n")
	}
	for _, c := range st.counts {
		// `status:<Name> (<n>)` is the distinctive token the spine item carries and
		// the progress page is expected to echo, so coverage can resolve it.
		fmt.Fprintf(&b, "- status:%s (%d) — %d issue(s) in %q\n", c.name, c.n, c.n, c.name)
	}

	b.WriteString("\n## Epics & progress\n\n")
	if len(st.epics) == 0 {
		b.WriteString("—\n")
	}
	for _, e := range st.epics {
		fmt.Fprintf(&b, "- epic:%s (%d%%) — %s: %d of %d done", e.key, e.pct(), e.summary, e.done, e.total)
		if len(e.statuses) > 0 {
			fmt.Fprintf(&b, " [%s]", joinCounts(e.statuses))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n## Blocked / in-flight\n\n")
	if len(st.blocked) == 0 && len(st.inFlight) == 0 {
		b.WriteString("Nothing blocked or in flight.\n")
	}
	for _, r := range st.blocked {
		fmt.Fprintf(&b, "- blocked: %s — %s (%s)\n", r.key, r.summary, r.status)
	}
	for _, r := range st.inFlight {
		fmt.Fprintf(&b, "- in-flight: %s — %s (%s)\n", r.key, r.summary, r.status)
	}

	b.WriteString("\n## Recent changes\n\n")
	fmt.Fprintf(&b, "- Updated in the last 7 days: %d; last 30 days: %d.\n", st.recent7, st.recent30)
	fmt.Fprintf(&b, "- Created in the last 7 days: %d; last 30 days: %d.\n", st.created7, st.created30)

	return b.String()
}

func joinCounts(cs []statusCount) string {
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = fmt.Sprintf("%s %d", c.name, c.n)
	}
	return strings.Join(parts, ", ")
}
