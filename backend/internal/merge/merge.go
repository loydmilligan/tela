// Package merge implements the server-side three-way merge that is the keystone
// of tela's file sync (sync spec §5). Body text merges line-based (markdown is
// line-oriented); props merge field-by-field; a title merges as a scalar. Each
// returns the merged value plus whatever truly conflicted, so the caller can
// keep both sides as revisions and flag the page — it never writes <<< markers
// into the content (those would corrupt the markdown and sync straight back).
//
// The algorithm is classic diff3: compute the LCS matching blocks of the base
// against each side, turn the gaps into change regions, and walk them — a region
// changed by only one side takes that side; a region both sides changed the same
// way takes it once; a region both changed differently is a conflict that takes
// the preferred side. Regions are merged only on true overlap (not adjacency),
// so independent edits on neighbouring lines auto-merge instead of colliding.
package merge

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// Side selects which version wins a conflict (an overlapping change both sides
// made differently). Both sides are always preserved by the caller as revisions
// regardless; this only decides what the merged text shows.
type Side int

const (
	// PreferIncoming makes the inbound file edit win a conflict hunk (the
	// just-typed local change stays visible; the DB side is recoverable).
	PreferIncoming Side = iota
	// PreferCurrent makes the server/DB version win a conflict hunk.
	PreferCurrent
)

// Conflict is one region where current and incoming both changed the same base
// lines differently. The merged output already contains the winning side; this
// records both so the caller can snapshot them.
type Conflict struct {
	Current  []string
	Incoming []string
}

// Merge3 three-way merges current and incoming against their common base,
// line-by-line. Returns the merged text and the conflicts (empty = clean).
// Splitting on "\n" makes a trailing newline a trailing empty line, so an
// unchanged side round-trips byte-for-byte.
func Merge3(base, current, incoming string, prefer Side) (string, []Conflict) {
	// Cheap, obviously-correct short-circuits.
	switch {
	case current == incoming:
		return current, nil
	case base == current:
		return incoming, nil
	case base == incoming:
		return current, nil
	}

	o := splitLines(base)
	a := splitLines(current)
	b := splitLines(incoming)

	atA := alignment(o, a)
	atB := alignment(o, b)

	hunks := append(diffRegions(o, a, 0), diffRegions(o, b, 1)...)
	sort.Slice(hunks, func(i, j int) bool {
		if hunks[i].baseLo != hunks[j].baseLo {
			return hunks[i].baseLo < hunks[j].baseLo
		}
		if hunks[i].baseHi != hunks[j].baseHi {
			return hunks[i].baseHi < hunks[j].baseHi // a pure insertion sorts before a change at the same point
		}
		return hunks[i].side < hunks[j].side
	})

	var out []string
	var conflicts []Conflict
	baseIdx := 0
	for i := 0; i < len(hunks); {
		lo, hi := hunks[i].baseLo, hunks[i].baseHi
		j := i + 1
		for j < len(hunks) && hunks[j].baseLo < hi { // strict: overlap, not adjacency
			if hunks[j].baseHi > hi {
				hi = hunks[j].baseHi
			}
			j++
		}
		out = append(out, o[baseIdx:lo]...) // stable run before the region

		if j == i+1 {
			// Exactly one hunk → only that side changed this region; the other side
			// still matches base, so take this side's lines verbatim (using its own
			// exact range, which is the only way to attribute an insertion to the
			// right side when both sides insert at the same base position).
			h := hunks[i]
			if h.side == 0 {
				out = append(out, a[h.sideLo:h.sideHi]...)
			} else {
				out = append(out, b[h.sideLo:h.sideHi]...)
			}
		} else {
			// Overlapping change from both sides → reconcile the full region span.
			// Region boundaries lo-1 and hi are stable in both sides, so the
			// alignment maps them cleanly into each side's coordinates.
			aLo, aHi := spanInOther(atA, lo, hi, len(a))
			bLo, bHi := spanInOther(atB, lo, hi, len(b))
			merged, conf := reconcile(o[lo:hi], a[aLo:aHi], b[bLo:bHi], prefer)
			out = append(out, merged...)
			if conf != nil {
				conflicts = append(conflicts, *conf)
			}
		}
		baseIdx = hi
		i = j
	}
	out = append(out, o[baseIdx:]...) // trailing stable run

	return strings.Join(out, "\n"), conflicts
}

// spanInOther maps a base region [lo,hi) — whose boundaries are stable in this
// side — to the corresponding [start,end) range in other. The start sits just
// after base[lo-1]'s aligned position (so insertions at lo are included); the
// end is base[hi]'s aligned position (so insertions at hi, which belong to the
// next region, are excluded).
func spanInOther(at []int, lo, hi, otherLen int) (start, end int) {
	if lo > 0 {
		start = at[lo-1] + 1
	}
	end = otherLen
	if hi < len(at)-1 {
		end = at[hi]
	}
	return start, end
}

// reconcile decides one change region. curSeg is current's lines for the region,
// incSeg incoming's, baseSeg the base's.
func reconcile(baseSeg, curSeg, incSeg []string, prefer Side) ([]string, *Conflict) {
	switch {
	case equalLines(incSeg, baseSeg):
		return curSeg, nil // only current changed here
	case equalLines(curSeg, baseSeg):
		return incSeg, nil // only incoming changed here
	case equalLines(curSeg, incSeg):
		return curSeg, nil // both made the same change
	default:
		conf := &Conflict{Current: cloneLines(curSeg), Incoming: cloneLines(incSeg)}
		if prefer == PreferCurrent {
			return curSeg, conf
		}
		return incSeg, conf
	}
}

type region struct {
	baseLo, baseHi int // range changed in base
	sideLo, sideHi int // the corresponding range in this side (the replacement lines)
	side           int // 0 = current, 1 = incoming
}

// diffRegions returns the base ranges where other differs from base (the
// complement of the matching blocks), each tagged with its exact side range. A
// pure insertion — other advanced while base did not — yields an empty base
// range (baseLo == baseHi) with a non-empty side range.
func diffRegions(base, other []string, side int) []region {
	var regs []region
	bi, oi := 0, 0
	for _, blk := range matchingBlocks(base, other) {
		if blk.bs > bi || blk.os > oi {
			regs = append(regs, region{baseLo: bi, baseHi: blk.bs, sideLo: oi, sideHi: blk.os, side: side})
		}
		bi = blk.bs + blk.size
		oi = blk.os + blk.size
	}
	return regs
}

// alignment returns, for each base index 0..len(base), the aligned index in
// other — so other[alignment[lo]:alignment[hi]] is the slice of other that
// corresponds to base[lo:hi].
func alignment(base, other []string) []int {
	at := make([]int, len(base)+1)
	bi, oi := 0, 0
	for _, blk := range matchingBlocks(base, other) {
		for ; bi < blk.bs; bi++ {
			at[bi] = oi // unmatched base line aligns to the current other cursor
		}
		for k := 0; k < blk.size; k++ {
			at[bi] = blk.os + k
			bi++
		}
		oi = blk.os + blk.size
	}
	at[len(base)] = len(other)
	return at
}

type block struct {
	bs, os, size int // base start, other start, run length
}

// matchingBlocks groups the LCS pairs of base and other into maximal contiguous
// runs and appends a zero-length sentinel at (len(base), len(other)).
func matchingBlocks(base, other []string) []block {
	pairs := lcsPairs(base, other)
	var blocks []block
	for _, p := range pairs {
		if n := len(blocks); n > 0 && blocks[n-1].bs+blocks[n-1].size == p.x && blocks[n-1].os+blocks[n-1].size == p.y {
			blocks[n-1].size++
		} else {
			blocks = append(blocks, block{bs: p.x, os: p.y, size: 1})
		}
	}
	return append(blocks, block{bs: len(base), os: len(other), size: 0})
}

type pair struct{ x, y int }

// lcsPairs returns the matched (baseIdx, otherIdx) pairs of a longest common
// subsequence, ascending. O(n*m) DP — fine for page-sized inputs (the caller
// guards against pathological sizes before merging).
func lcsPairs(x, y []string) []pair {
	n, m := len(x), len(y)
	if n == 0 || m == 0 {
		return nil
	}
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var pairs []pair
	i, j := 0, 0
	for i < n && j < m {
		if x[i] == y[j] {
			pairs = append(pairs, pair{i, j})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			i++
		} else {
			j++
		}
	}
	return pairs
}

func splitLines(s string) []string { return strings.Split(s, "\n") }

func equalLines(a, b []string) bool {
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

func cloneLines(a []string) []string {
	if len(a) == 0 {
		return nil
	}
	out := make([]string, len(a))
	copy(out, a)
	return out
}

// MergeProps three-way merges the props bag field-by-field (sync spec §5: props
// are structured-merged, not text-diffed). A key changed by only one side takes
// that side (including deletion); both sides changing it the same way is no
// conflict; a divergent change takes prefer and is reported. Returns the merged
// bag and the conflicting keys.
func MergeProps(base, current, incoming map[string]any, prefer Side) (map[string]any, []string) {
	out := map[string]any{}
	var conflicts []string
	for _, k := range unionKeys(base, current, incoming) {
		bv, bok := base[k]
		cv, cok := current[k]
		iv, iok := incoming[k]
		curChanged := !sameVal(bv, bok, cv, cok)
		incChanged := !sameVal(bv, bok, iv, iok)
		switch {
		case !incChanged:
			if cok {
				out[k] = cv
			}
		case !curChanged:
			if iok {
				out[k] = iv
			}
		case sameVal(cv, cok, iv, iok):
			if cok {
				out[k] = cv
			}
		default:
			conflicts = append(conflicts, k)
			if prefer == PreferCurrent {
				if cok {
					out[k] = cv
				}
			} else if iok {
				out[k] = iv
			}
		}
	}
	return out, conflicts
}

// Scalar three-way merges a single value (the page title). conflicted is true
// when both sides changed it to different values.
func Scalar(base, current, incoming string, prefer Side) (merged string, conflicted bool) {
	switch {
	case current == incoming:
		return current, false
	case current == base:
		return incoming, false
	case incoming == base:
		return current, false
	default:
		if prefer == PreferCurrent {
			return current, true
		}
		return incoming, true
	}
}

func unionKeys(maps ...map[string]any) []string {
	seen := map[string]bool{}
	var keys []string
	for _, m := range maps {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

// sameVal reports whether two (value, present) props entries are equal, by
// JSON-canonical comparison so a YAML int and a JSONB float of the same value
// match (mirrors the api-layer propsEqual). Absent on both = equal.
func sameVal(a any, aok bool, b any, bok bool) bool {
	if aok != bok {
		return false
	}
	if !aok {
		return true
	}
	ja, err1 := json.Marshal(a)
	jb, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return bytes.Equal(ja, jb)
}
