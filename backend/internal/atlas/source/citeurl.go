package source

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zcag/tela/backend/internal/atlas/core"
)

// CiteURL maps a resolved `file:line` citation to a clickable source URL for the
// given source. It is type-switched on src.Type:
//
//   - git:  a GitHub blob URL at the pinned ref with a line anchor. Only GitHub
//     is supported for now; a non-github or unparseable Location returns "".
//   - jira: the issue's browse URL, derived from an `issues/<KEY>.md` path (no
//     line anchor). A path that isn't an issue file returns "".
//
// Returning "" signals the linkifier to leave the citation as plain text.
func CiteURL(src core.Source, file string, l1, l2 int) string {
	switch src.Type {
	case core.SourceGit:
		owner, repo, ok := githubOwnerRepo(src.Location)
		if !ok {
			return ""
		}
		ref := src.Ref
		if ref == "" {
			ref = "HEAD"
		}
		u := fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s#L%d", owner, repo, ref, file, l1)
		if l2 > l1 {
			u += fmt.Sprintf("-L%d", l2)
		}
		return u
	case core.SourceJira:
		key, ok := jiraIssueKey(file)
		if !ok {
			return ""
		}
		base := strings.TrimRight(src.Location, "/")
		if base == "" {
			return ""
		}
		return base + "/browse/" + key
	default:
		return ""
	}
}

// githubURLRe matches both https and ssh GitHub remotes, capturing owner/repo
// (an optional .git suffix is stripped by the (?:\.git)? group).
var githubURLRe = regexp.MustCompile(`^(?:https?://github\.com/|git@github\.com:)([^/]+)/(.+?)(?:\.git)?/?$`)

// githubOwnerRepo extracts owner/repo from a GitHub https or ssh remote.
func githubOwnerRepo(loc string) (owner, repo string, ok bool) {
	m := githubURLRe.FindStringSubmatch(strings.TrimSpace(loc))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// jiraIssueRe matches the `issues/<KEY>.md` path a Jira issue page is cited as.
var jiraIssueRe = regexp.MustCompile(`(?:^|/)issues/([^/]+)\.md$`)

// jiraIssueKey returns the issue KEY from an `issues/<KEY>.md` citation path.
func jiraIssueKey(file string) (key string, ok bool) {
	m := jiraIssueRe.FindStringSubmatch(file)
	if m == nil {
		return "", false
	}
	return m[1], true
}
