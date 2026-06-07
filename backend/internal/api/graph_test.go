package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// TestGraphData covers the three things that matter: nodes are membership-scoped
// (no cross-space leak), link edges come from page_links, and tree edges come
// from parent_id — both only when BOTH endpoints are visible.
func TestGraphData(t *testing.T) {
	ctx := context.Background()
	d := newAPITestDB(t)
	srv := New(d)

	owner := seedUser(t, d, "owner", "ownerpw123", false)
	stranger := seedUser(t, d, "stranger", "strangerpw", false)
	mine := seedSpace(t, d, "Mine", "mine", owner)
	other := seedSpace(t, d, "Other", "other", stranger) // owner is NOT a member

	// In "Mine": parent → child, and child links to a sibling.
	var parent, child, sibling int64
	if err := d.QueryRowContext(ctx, `INSERT INTO pages(space_id, parent_id, title, body, position) VALUES ($1, NULL, 'Parent', '', 0) RETURNING id`, mine).Scan(&parent); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := d.QueryRowContext(ctx, `INSERT INTO pages(space_id, parent_id, title, body, position) VALUES ($1, $2, 'Child', '', 0) RETURNING id`, mine, parent).Scan(&child); err != nil {
		t.Fatalf("seed child: %v", err)
	}
	if err := d.QueryRowContext(ctx, `INSERT INTO pages(space_id, parent_id, title, body, position) VALUES ($1, NULL, 'Sibling', '', 1) RETURNING id`, mine).Scan(&sibling); err != nil {
		t.Fatalf("seed sibling: %v", err)
	}
	// A page in a space owner can't see.
	var secret int64
	if err := d.QueryRowContext(ctx, `INSERT INTO pages(space_id, parent_id, title, body, position) VALUES ($1, NULL, 'Secret', '', 0) RETURNING id`, other).Scan(&secret); err != nil {
		t.Fatalf("seed secret: %v", err)
	}

	body := fmt.Sprintf("links to [s](tela://page/%d)", sibling)
	tx, _ := d.BeginTx(ctx, nil)
	if err := syncPageLinks(ctx, tx, child, body); err != nil {
		tx.Rollback()
		t.Fatalf("syncPageLinks: %v", err)
	}
	// Sibling links to a non-existent page → a broken outgoing link.
	if err := syncPageLinks(ctx, tx, sibling, "dead [x](tela://page/999999)"); err != nil {
		tx.Rollback()
		t.Fatalf("syncPageLinks broken: %v", err)
	}
	tx.Commit()

	req := userRequest(http.MethodGet, "/api/graph", "", authUser(owner, "owner", false))
	rec := recordHandler(srv.GraphData, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var out struct {
		Nodes []graphNode `json:"nodes"`
		Links []graphLink `json:"links"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Nodes: exactly the 3 visible pages, never the secret one.
	if len(out.Nodes) != 3 {
		t.Fatalf("nodes=%d want 3: %+v", len(out.Nodes), out.Nodes)
	}
	for _, n := range out.Nodes {
		if n.ID == secret {
			t.Fatalf("secret page leaked into graph nodes")
		}
		if n.UpdatedAt == "" {
			t.Fatalf("node %d missing updated_at", n.ID)
		}
		if n.ID == sibling && n.Broken != 1 {
			t.Fatalf("sibling broken=%d want 1", n.Broken)
		}
		if n.ID != sibling && n.Broken != 0 {
			t.Fatalf("node %d broken=%d want 0", n.ID, n.Broken)
		}
	}

	// Edges: one tree (parent→child) and one link (child→sibling).
	var trees, links int
	for _, e := range out.Links {
		switch e.Kind {
		case "tree":
			trees++
			if e.Source != parent || e.Target != child {
				t.Fatalf("tree edge = %d→%d want %d→%d", e.Source, e.Target, parent, child)
			}
		case "link":
			links++
			if e.Source != child || e.Target != sibling {
				t.Fatalf("link edge = %d→%d want %d→%d", e.Source, e.Target, child, sibling)
			}
		default:
			t.Fatalf("unexpected edge kind %q", e.Kind)
		}
	}
	if trees != 1 || links != 1 {
		t.Fatalf("trees=%d links=%d want 1 and 1 (links=%+v)", trees, links, out.Links)
	}
}

// TestGraphData_SpaceFilter narrows to one space via ?space_id=.
func TestGraphData_SpaceFilter(t *testing.T) {
	ctx := context.Background()
	d := newAPITestDB(t)
	srv := New(d)

	owner := seedUser(t, d, "owner", "ownerpw123", false)
	a := seedSpace(t, d, "A", "a", owner)
	b := seedSpace(t, d, "B", "b", owner)
	if _, err := d.ExecContext(ctx, `INSERT INTO pages(space_id, parent_id, title, body, position) VALUES ($1, NULL, 'PA', '', 0)`, a); err != nil {
		t.Fatalf("seed PA: %v", err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO pages(space_id, parent_id, title, body, position) VALUES ($1, NULL, 'PB', '', 0)`, b); err != nil {
		t.Fatalf("seed PB: %v", err)
	}

	req := userRequest(http.MethodGet, fmt.Sprintf("/api/graph?space_id=%d", a), "", authUser(owner, "owner", false))
	rec := recordHandler(srv.GraphData, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	var out struct {
		Nodes []graphNode `json:"nodes"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Nodes) != 1 || out.Nodes[0].SpaceID != a {
		t.Fatalf("space filter nodes=%+v want only space %d", out.Nodes, a)
	}
}
