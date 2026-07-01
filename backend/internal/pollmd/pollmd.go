// Package pollmd is the in-body encoding for poll votes. A poll is a
// `:::poll{id=...}` container directive whose options are top-level list items
// and whose voters are nested `- @username` items under the option they chose:
//
//	:::poll{id="offsite"}
//	### Where should we host the offsite?
//
//	- Lisbon
//	  - @ada
//	  - @alan
//	- Berlin
//	:::
//
// Votes live in pages.body (canonical markdown) — casting a vote is a structured
// edit of this block, not a side table. This package is the pure text surgery;
// the API layer wraps it with page-access checks and the collab-overlay reset so
// live editors re-seed from the new body.
package pollmd

import (
	"errors"
	"strings"
)

var (
	// ErrPollNotFound — no :::poll block with the given id in the body.
	ErrPollNotFound = errors.New("poll not found")
	// ErrOptionNotFound — the chosen option label isn't in the poll.
	ErrOptionNotFound = errors.New("poll option not found")
)

// ApplyVote records `username`'s vote for `choice` (an option label) in the poll
// `pollID` within `body`, returning the rewritten body. It first removes the
// user's existing vote anywhere in that poll (so changing a vote is a move), then
// adds it under the chosen option. An empty `choice` retracts the vote. `changed`
// is false when the body already reflected the vote (idempotent no-op).
func ApplyVote(body, pollID, choice, username string) (out string, changed bool, err error) {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	if username == "" {
		return body, false, errors.New("empty username")
	}
	// Preserve the document's newline flavor on the round-trip.
	nl := "\n"
	if strings.Contains(body, "\r\n") {
		nl = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")

	open, close := findBlock(lines, pollID)
	if open < 0 {
		return body, false, ErrPollNotFound
	}
	inner := lines[open+1 : close]

	// 1. Drop the user's current vote wherever it sits in this poll.
	kept := make([]string, 0, len(inner)+1)
	for _, ln := range inner {
		if voterOf(ln) == username {
			continue
		}
		kept = append(kept, ln)
	}

	// 2. Add it under the chosen option (unless retracting).
	if choice != "" {
		idx, indent, marker := findOption(kept, choice)
		if idx < 0 {
			return body, false, ErrOptionNotFound
		}
		// Match the option's own bullet marker so the vote looks native (the
		// editor serializes `*`, hand/agent authoring uses `-`).
		voter := indent + "  " + string(marker) + " @" + username
		kept = append(kept[:idx+1], append([]string{voter}, kept[idx+1:]...)...)
	}

	rebuilt := make([]string, 0, len(lines)+1)
	rebuilt = append(rebuilt, lines[:open+1]...)
	rebuilt = append(rebuilt, kept...)
	rebuilt = append(rebuilt, lines[close:]...)
	// `changed` is a real diff — remove-then-re-add nets to a no-op when the vote
	// was already where it's being cast (idempotent re-vote).
	result := strings.Join(rebuilt, nl)
	return result, result != body, nil
}

// findBlock returns the line indices of the `:::poll{id=pollID}` opening fence
// and its matching `:::` close, or (-1, -1) if absent.
func findBlock(lines []string, pollID string) (open, close int) {
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, ":::poll") {
			continue
		}
		if attrID(t) != pollID {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == ":::" {
				return i, j
			}
		}
		return -1, -1 // unterminated
	}
	return -1, -1
}

// attrID extracts the `id` from a directive opening line's `{...}` attributes,
// supporting `{id="x"}`, `{id=x}`, and the `{#x}` shorthand.
func attrID(line string) string {
	l, r := strings.IndexByte(line, '{'), strings.LastIndexByte(line, '}')
	if l < 0 || r <= l {
		return ""
	}
	for _, tok := range strings.Fields(line[l+1 : r]) {
		switch {
		case strings.HasPrefix(tok, "#"):
			return tok[1:]
		case strings.HasPrefix(tok, "id="):
			return strings.Trim(tok[3:], `"'`)
		}
	}
	return ""
}

// bulletItem reports whether a directive-trimmed line is a markdown bullet
// (`- x` / `* x` / `+ x`), returning its content and marker byte. Markdown
// allows all three markers, and the editor's serializer emits `*` where hand /
// agent authoring uses `-`, so the surgery must accept any.
func bulletItem(trimmed string) (content string, marker byte, ok bool) {
	if len(trimmed) >= 2 && (trimmed[0] == '-' || trimmed[0] == '*' || trimmed[0] == '+') && trimmed[1] == ' ' {
		return trimmed[2:], trimmed[0], true
	}
	return "", 0, false
}

// voterOf returns the username of a nested `- @username` voter line, or "" if the
// line isn't one (options, blanks, prose).
func voterOf(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) == len(line) { // no indent → an option, not a voter
		return ""
	}
	content, _, ok := bulletItem(trimmed)
	if !ok || !strings.HasPrefix(content, "@") {
		return ""
	}
	fields := strings.Fields(content)
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimPrefix(fields[0], "@")
}

// findOption locates a top-level option line whose label equals `choice`,
// returning its index, leading indent, and bullet marker.
func findOption(lines []string, choice string) (idx int, indent string, marker byte) {
	for i, ln := range lines {
		trimmed := strings.TrimLeft(ln, " \t")
		lead := ln[:len(ln)-len(trimmed)]
		if lead != "" { // indented → a voter, not an option
			continue
		}
		content, m, ok := bulletItem(trimmed)
		if !ok {
			continue
		}
		if strings.TrimSpace(content) == choice {
			return i, lead, m
		}
	}
	return -1, "", 0
}
