package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// jiraClient is a minimal Jira Cloud REST v3 client — just the reads atlas needs
// to materialize a project: paginated issue search, the field catalog, and
// project metadata. It mirrors internal/tela's client style (timeout, context
// requests, status-checked JSON).
type jiraClient struct {
	base  string
	auth  string // "Basic <b64>" when credentialed, else ""
	http  *http.Client
	pageN int // search page size
}

func newClient(baseURL, email, token string) *jiraClient {
	c := &jiraClient{
		base:  strings.TrimRight(baseURL, "/"),
		http:  &http.Client{Timeout: 30 * time.Second},
		pageN: 50, // rate-limit-friendly page size
	}
	if token != "" {
		c.auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+token))
	}
	return c
}

// --- API value shapes ------------------------------------------------------

type issue struct {
	Key    string      `json:"key"`
	Fields issueFields `json:"fields"`
}

type issueFields struct {
	Summary     string          `json:"summary"`
	Description json.RawMessage `json:"description"` // ADF doc or string
	Created     string          `json:"created"`
	Updated     string          `json:"updated"`
	IssueType   named           `json:"issuetype"`
	Status      named           `json:"status"`
	Priority    *named          `json:"priority"`
	Assignee    *user           `json:"assignee"`
	Reporter    *user           `json:"reporter"`
	Components  []named         `json:"components"`
	Labels      []string        `json:"labels"`
	FixVersions []named         `json:"fixVersions"`
	Parent      *parentRef      `json:"parent"`
	Comment     commentPage     `json:"comment"`
}

type named struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

type user struct {
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

type parentRef struct {
	Key    string `json:"key"`
	Fields struct {
		Summary   string `json:"summary"`
		IssueType named  `json:"issuetype"`
	} `json:"fields"`
}

type commentPage struct {
	Comments []comment `json:"comments"`
}

type comment struct {
	Author  user            `json:"author"`
	Created string          `json:"created"`
	Body    json.RawMessage `json:"body"` // ADF or string
}

// the new (2024+) Jira Cloud search API: POST /rest/api/3/search/jql with
// opaque token pagination. The old GET /rest/api/3/search was removed (HTTP 410).
type searchJQLReq struct {
	JQL           string   `json:"jql"`
	Fields        []string `json:"fields"`
	MaxResults    int      `json:"maxResults"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
}
type searchJQLResp struct {
	Issues        []issue `json:"issues"`
	NextPageToken string  `json:"nextPageToken"`
	IsLast        bool    `json:"isLast"`
}

// the fields we render for each issue (keeps the search payload small + stable).
var issueFieldList = "summary,description,created,updated,issuetype,status,priority," +
	"assignee,reporter,components,labels,fixVersions,parent,comment"

// searchAll pages through POST /rest/api/3/search/jql for a JQL query,
// accumulating every issue via opaque next-page tokens until isLast.
func (c *jiraClient) searchAll(ctx context.Context, jql string) ([]issue, error) {
	fields := strings.Split(issueFieldList, ",")
	var out []issue
	token := ""
	for {
		var res searchJQLResp
		body := searchJQLReq{JQL: jql, Fields: fields, MaxResults: c.pageN, NextPageToken: token}
		if err := c.post(ctx, "/rest/api/3/search/jql", body, &res); err != nil {
			return nil, err
		}
		out = append(out, res.Issues...)
		if res.IsLast || res.NextPageToken == "" || len(res.Issues) == 0 {
			break
		}
		token = res.NextPageToken
	}
	return out, nil
}

// countSince runs one small search page for jql (maxResults=1, key field only)
// and reports how many issues it saw — enough for the HasChanges probe to answer
// "is there anything new?" without paging the whole result set.
func (c *jiraClient) countSince(ctx context.Context, jql string) (int, error) {
	var res searchJQLResp
	body := searchJQLReq{JQL: jql, Fields: []string{"key"}, MaxResults: 1}
	if err := c.post(ctx, "/rest/api/3/search/jql", body, &res); err != nil {
		return 0, err
	}
	return len(res.Issues), nil
}

// projectMeta bundles a project's schema-defining metadata.
type projectMetaData struct {
	IssueTypes  []named
	Components  []named
	Versions    []named
	Statuses    []named // distinct status names across all issue types
	CustomField []named // user-defined fields (custom:*)
}

// projectMeta fetches the project's schema surface: issue types, components and
// versions from the project resource, statuses from the per-issuetype status
// map, and the custom-field subset of the global field catalog.
func (c *jiraClient) projectMeta(ctx context.Context, key string) (projectMetaData, error) {
	var meta projectMetaData

	var proj struct {
		IssueTypes []named `json:"issueTypes"`
		Components []named `json:"components"`
		Versions   []named `json:"versions"`
	}
	if err := c.get(ctx, "/rest/api/3/project/"+url.PathEscape(key), &proj); err != nil {
		return meta, err
	}
	meta.IssueTypes = proj.IssueTypes
	meta.Components = proj.Components
	meta.Versions = proj.Versions

	// statuses are returned grouped per issue type; flatten + dedupe by name.
	var statusGroups []struct {
		Statuses []named `json:"statuses"`
	}
	if err := c.get(ctx, "/rest/api/3/project/"+url.PathEscape(key)+"/statuses", &statusGroups); err != nil {
		return meta, err
	}
	seen := map[string]bool{}
	for _, g := range statusGroups {
		for _, s := range g.Statuses {
			if !seen[s.Name] {
				seen[s.Name] = true
				meta.Statuses = append(meta.Statuses, s)
			}
		}
	}

	// custom fields from the global field catalog.
	var fields []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Custom bool   `json:"custom"`
	}
	if err := c.get(ctx, "/rest/api/3/field", &fields); err != nil {
		return meta, err
	}
	for _, f := range fields {
		if f.Custom {
			meta.CustomField = append(meta.CustomField, named{Name: f.Name, ID: f.ID})
		}
	}
	return meta, nil
}

func (c *jiraClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.auth != "" {
		req.Header.Set("Authorization", c.auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("jira GET %s: http %d: %s", path, resp.StatusCode, snippet(data))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (c *jiraClient) post(ctx context.Context, path string, body, out any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if c.auth != "" {
		req.Header.Set("Authorization", c.auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("jira POST %s: http %d: %s", path, resp.StatusCode, snippet(data))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
