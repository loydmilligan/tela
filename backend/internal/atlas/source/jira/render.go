package jira

import (
	"encoding/json"
	"fmt"
	"strings"
)

// renderIssue turns one Jira issue into a markdown document: a title, a metadata
// block (type, status, priority, assignee, components, labels, fix versions,
// parent/epic), the description, and the comment thread. This is the retrievable
// unit the shared pipeline chunks + embeds — the same role a source file plays
// for git.
func renderIssue(is issue) string {
	f := is.Fields
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: %s\n\n", is.Key, f.Summary)

	field := func(label, val string) {
		if strings.TrimSpace(val) != "" {
			fmt.Fprintf(&b, "- **%s:** %s\n", label, val)
		}
	}
	field("Type", f.IssueType.Name)
	field("Status", f.Status.Name)
	if f.Priority != nil {
		field("Priority", f.Priority.Name)
	}
	if f.Assignee != nil {
		field("Assignee", f.Assignee.DisplayName)
	}
	if f.Reporter != nil {
		field("Reporter", f.Reporter.DisplayName)
	}
	field("Components", joinNames(f.Components))
	field("Labels", strings.Join(f.Labels, ", "))
	field("Fix Versions", joinNames(f.FixVersions))
	if f.Parent != nil {
		field("Parent", fmt.Sprintf("%s (%s)", f.Parent.Key, f.Parent.Fields.Summary))
	}
	field("Created", f.Created)
	field("Updated", f.Updated)

	if desc := strings.TrimSpace(extractText(f.Description)); desc != "" {
		b.WriteString("\n## Description\n\n")
		b.WriteString(desc)
		b.WriteString("\n")
	}

	if len(f.Comment.Comments) > 0 {
		b.WriteString("\n## Comments\n\n")
		for _, cm := range f.Comment.Comments {
			author := cm.Author.DisplayName
			if author == "" {
				author = "unknown"
			}
			fmt.Fprintf(&b, "**%s** (%s):\n\n%s\n\n", author, cm.Created, strings.TrimSpace(extractText(cm.Body)))
		}
	}
	return b.String()
}

// renderSchema writes the project's surface metadata as schema.md. The section
// headings + list shape are the contract parseSchemaSpine reads back, so the
// spine is a deterministic function of this file (no second fetch).
func renderSchema(key string, m projectMetaData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Jira Project %s — Schema\n\n", key)
	b.WriteString("The deterministic surface inventory for this project: the issue types, ")
	b.WriteString("statuses, components, custom fields, versions, and epics a complete ")
	b.WriteString("knowledge base must cover.\n")

	section(&b, "Issue Types", names(m.IssueTypes))
	section(&b, "Statuses", names(m.Statuses))
	section(&b, "Components", names(m.Components))
	section(&b, "Custom Fields", names(m.CustomField))
	section(&b, "Versions", names(m.Versions))
	section(&b, "Epics", epicNames(m.IssueTypes))
	return b.String()
}

// section writes a "## Title" heading and a "- item" list (or an em-dash when
// empty), keeping the schema readable + spine-parseable.
func section(b *strings.Builder, title string, items []string) {
	fmt.Fprintf(b, "\n## %s\n\n", title)
	if len(items) == 0 {
		b.WriteString("—\n")
		return
	}
	for _, it := range items {
		if strings.TrimSpace(it) != "" {
			fmt.Fprintf(b, "- %s\n", it)
		}
	}
}

// epicNames derives the Epic surface from the issue types: if the project has an
// "Epic" issue type it's a covered surface element. (Listing every epic issue
// would belong in the issues, not the schema; the spine just needs the kind to
// exist as a must-cover surface.)
func epicNames(types []named) []string {
	for _, t := range types {
		if strings.EqualFold(t.Name, "Epic") {
			return []string{"Epic"}
		}
	}
	return nil
}

func names(ns []named) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		if n.Name != "" {
			out = append(out, n.Name)
		}
	}
	return out
}

func joinNames(ns []named) string { return strings.Join(names(ns), ", ") }

// --- ADF (Atlassian Document Format) → text --------------------------------

// extractText flattens a Jira rich-text field into plain text. Jira Cloud sends
// descriptions/comments as ADF (a nested JSON document); older/server payloads
// send a plain string. We pull the visible text nodes (paragraphs separated by
// blank lines, list items prefixed) — enough for grounded retrieval without
// reimplementing a full ADF renderer.
func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// plain string field?
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var node adfNode
	if json.Unmarshal(raw, &node) != nil {
		return ""
	}
	var b strings.Builder
	walkADF(&b, node)
	return strings.TrimRight(b.String(), "\n")
}

type adfNode struct {
	Type    string    `json:"type"`
	Text    string    `json:"text"`
	Content []adfNode `json:"content"`
}

func walkADF(b *strings.Builder, n adfNode) {
	switch n.Type {
	case "text":
		b.WriteString(n.Text)
	case "hardBreak":
		b.WriteString("\n")
	case "listItem":
		b.WriteString("- ")
		for _, c := range n.Content {
			walkADF(b, c)
		}
		b.WriteString("\n")
		return
	}
	for _, c := range n.Content {
		walkADF(b, c)
	}
	// block-level nodes end with a blank line so paragraphs/headings separate.
	switch n.Type {
	case "paragraph", "heading", "blockquote", "codeBlock":
		b.WriteString("\n\n")
	}
}
