package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// mkCountPage inserts a page in a space and returns its id.
func mkCountPage(t *testing.T, d *sql.DB, spaceID int64, title string) int64 {
	t.Helper()
	var id int64
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO pages (space_id, parent_id, title, body, position)
		 VALUES ($1, NULL, $2, 'b', 0) RETURNING id`, spaceID, title).Scan(&id); err != nil {
		t.Fatalf("insert page %s: %v", title, err)
	}
	return id
}

// markDisputed writes a clean page_agreement row with dispute>0 (the same shape
// the agreement worker produces).
func markDisputed(t *testing.T, d *sql.DB, pageID int64, n int) {
	t.Helper()
	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO page_agreement (page_id, src_hash, model, dispute, last_error)
		 VALUES ($1, 'h', 'm', $2, '')`, pageID, n); err != nil {
		t.Fatalf("mark disputed %d: %v", pageID, err)
	}
}

func spaceCountsFor(t *testing.T, c *http.Client, tsURL string) map[int64]spaceCounts {
	t.Helper()
	resp, err := c.Get(tsURL + "/api/spaces/counts")
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("counts: status=%d body=%s", resp.StatusCode, b)
	}
	var env struct {
		Spaces []spaceCounts `json:"spaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode counts: %v", err)
	}
	m := map[int64]spaceCounts{}
	for _, s := range env.Spaces {
		m[s.SpaceID] = s
	}
	return m
}

// TestSpaceCounts covers total/disputed correctness and — the load-bearing part
// — that a space the caller can't read never appears in the counts, so a count
// can't leak a private space's existence or size.
func TestSpaceCounts(t *testing.T) {
	ts, d := newWiredServer(t)
	owner := seedUser(t, d, "scowner", "ownerpw12", false)
	outsider := seedUser(t, d, "scout", "outpw123456", false)

	alpha := seedSpace(t, d, "Alpha", "sc-alpha", owner)
	bravo := seedSpace(t, d, "Bravo", "sc-bravo", owner) // owner-only

	// Alpha: 3 pages, one disputed. Bravo: 2 pages, one disputed.
	mkCountPage(t, d, alpha, "A1")
	a2 := mkCountPage(t, d, alpha, "A2")
	mkCountPage(t, d, alpha, "A3")
	markDisputed(t, d, a2, 2)

	mkCountPage(t, d, bravo, "B1")
	b2 := mkCountPage(t, d, bravo, "B2")
	markDisputed(t, d, b2, 1)

	oc := loginClient(t, ts, "scowner", "ownerpw12")

	t.Run("owner sees total and disputed for both spaces", func(t *testing.T) {
		m := spaceCountsFor(t, oc, ts.URL)
		if m[alpha].Total != 3 || m[alpha].Disputed != 1 {
			t.Fatalf("Alpha counts = %+v, want total 3 disputed 1", m[alpha])
		}
		if m[bravo].Total != 2 || m[bravo].Disputed != 1 {
			t.Fatalf("Bravo counts = %+v, want total 2 disputed 1", m[bravo])
		}
	})

	t.Run("a space with zero pages still reports total 0", func(t *testing.T) {
		empty := seedSpace(t, d, "Empty", "sc-empty", owner)
		m := spaceCountsFor(t, oc, ts.URL)
		c, ok := m[empty]
		if !ok {
			t.Fatalf("empty space missing from counts (should appear with total 0)")
		}
		if c.Total != 0 || c.Disputed != 0 {
			t.Fatalf("empty space counts = %+v, want zeros", c)
		}
	})

	// THE load-bearing test: an outsider granted Alpha only must see Alpha's
	// counts and NOT Bravo's — a count can't leak a private space.
	t.Run("counts never leak a space the caller can't read", func(t *testing.T) {
		seedMember(t, d, alpha, outsider, "viewer") // Alpha only, NOT Bravo
		xc := loginClient(t, ts, "scout", "outpw123456")
		m := spaceCountsFor(t, xc, ts.URL)
		if _, ok := m[bravo]; ok {
			t.Fatalf("Bravo (outsider is not a member) leaked into counts: %+v", m[bravo])
		}
		if m[alpha].Total != 3 || m[alpha].Disputed != 1 {
			t.Fatalf("Alpha counts for viewer = %+v, want total 3 disputed 1", m[alpha])
		}
	})
}
