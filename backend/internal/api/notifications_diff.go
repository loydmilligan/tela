package api

import (
	"fmt"
	"strings"

	"github.com/zcag/tela/backend/internal/mailer"
)

// changePreview builds a compact "what changed" diff for a page_updated email:
// a line-level LCS over old vs new body, returning up to changePreviewMax
// changed lines (in document order), a "12 additions · 3 deletions" stat, and a
// "…N more changed lines" overflow note. Empty when nothing meaningful changed.
//
// Bounded: very large bodies skip the O(n·m) LCS and return a stat-only preview
// (no per-line block), so a huge edit can't blow up the send path.
const (
	changePreviewMax   = 6   // changed lines shown inline
	changePreviewBound = 800 // per-side line cap before we fall back to stat-only
	changeLineMax      = 100 // per-line character cap
)

func changePreview(oldBody, newBody string) (lines []mailer.DiffLine, stat, more string) {
	if oldBody == newBody {
		return nil, "", ""
	}
	a := strings.Split(oldBody, "\n")
	b := strings.Split(newBody, "\n")
	if len(a) > changePreviewBound || len(b) > changePreviewBound {
		// Too large for an inline diff — just say how much moved.
		add, del := roughLineDelta(a, b)
		return nil, diffStat(add, del), "Open the page to see the full diff."
	}

	ops := lineDiffOps(a, b) // ordered: each is (add bool, text string)
	var totalAdd, totalDel int
	for _, op := range ops {
		if op.add {
			totalAdd++
		} else {
			totalDel++
		}
	}
	if totalAdd == 0 && totalDel == 0 {
		return nil, "", ""
	}

	shown := 0
	for _, op := range ops {
		if shown >= changePreviewMax {
			break
		}
		lines = append(lines, mailer.DiffLine{Add: op.add, Text: trimLine(op.text)})
		shown++
	}
	if rem := (totalAdd + totalDel) - shown; rem > 0 {
		more = fmt.Sprintf("…and %d more changed line%s", rem, plural(rem))
	}
	return lines, diffStat(totalAdd, totalDel), more
}

type diffOp struct {
	add  bool
	text string
}

// lineDiffOps returns the changed lines (additions + deletions, no context) in
// document order via a standard LCS. Blank-only changes are dropped as noise.
func lineDiffOps(a, b []string) []diffOp {
	n, m := len(a), len(b)
	// LCS length table.
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			if strings.TrimSpace(a[i]) != "" {
				ops = append(ops, diffOp{add: false, text: a[i]})
			}
			i++
		default:
			if strings.TrimSpace(b[j]) != "" {
				ops = append(ops, diffOp{add: true, text: b[j]})
			}
			j++
		}
	}
	for ; i < n; i++ {
		if strings.TrimSpace(a[i]) != "" {
			ops = append(ops, diffOp{add: false, text: a[i]})
		}
	}
	for ; j < m; j++ {
		if strings.TrimSpace(b[j]) != "" {
			ops = append(ops, diffOp{add: true, text: b[j]})
		}
	}
	return ops
}

// roughLineDelta is the cheap fallback for oversized bodies: a multiset count of
// lines present on only one side (no ordering, no LCS).
func roughLineDelta(a, b []string) (add, del int) {
	count := func(ls []string) map[string]int {
		m := map[string]int{}
		for _, l := range ls {
			if strings.TrimSpace(l) != "" {
				m[l]++
			}
		}
		return m
	}
	ca, cb := count(a), count(b)
	for l, nb := range cb {
		if d := nb - ca[l]; d > 0 {
			add += d
		}
	}
	for l, na := range ca {
		if d := na - cb[l]; d > 0 {
			del += d
		}
	}
	return add, del
}

func diffStat(add, del int) string {
	switch {
	case add > 0 && del > 0:
		return fmt.Sprintf("%d addition%s · %d deletion%s", add, plural(add), del, plural(del))
	case add > 0:
		return fmt.Sprintf("%d addition%s", add, plural(add))
	case del > 0:
		return fmt.Sprintf("%d deletion%s", del, plural(del))
	}
	return ""
}

func trimLine(s string) string {
	s = strings.TrimRight(s, " \t")
	if len(s) > changeLineMax {
		s = strings.TrimSpace(s[:changeLineMax]) + "…"
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
