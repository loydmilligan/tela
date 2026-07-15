package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/zcag/tela/backend/internal/models"
)

// decodePage pulls the {"page": …} envelope out of a response body.
func decodePage(t *testing.T, resp *http.Response) models.Page {
	t.Helper()
	defer resp.Body.Close()
	var env struct {
		Page models.Page `json:"page"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	return env.Page
}

func getPageHTTP(t *testing.T, c *http.Client, tsURL string, id int64) models.Page {
	t.Helper()
	resp, err := c.Get(fmt.Sprintf("%s/api/pages/%d", tsURL, id))
	if err != nil {
		t.Fatalf("get page: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("get page: status=%d body=%s", resp.StatusCode, b)
	}
	return decodePage(t, resp)
}

// TestPageProps_CRUD exercises the Phase-1 props lifecycle end to end:
// create with props, body-frontmatter absorption + the body invariant,
// reserved-key drop on the explicit field, Replace update semantics, and a
// props-only update.
func TestPageProps_CRUD(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	space := seedSpace(t, d, "S", "s", admin)
	c := loginClient(t, ts, "admin", "adminpw12")

	mustCreate := func(body string) models.Page {
		t.Helper()
		resp, err := postJSON(c, ts.URL+"/api/pages", body)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if resp.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("create: status=%d body=%s", resp.StatusCode, b)
		}
		return decodePage(t, resp)
	}

	t.Run("create with explicit props; reserved keys dropped", func(t *testing.T) {
		p := mustCreate(fmt.Sprintf(
			`{"space_id":%d,"title":"P1","body":"hello","props":{"status":"draft","id":999,"title":"nope"}}`, space))
		got := getPageHTTP(t, c, ts.URL, p.ID)
		if got.Props["status"] != "draft" {
			t.Fatalf("props.status = %v, want draft", got.Props["status"])
		}
		if _, ok := got.Props["id"]; ok {
			t.Fatalf("reserved key id leaked into props: %#v", got.Props)
		}
		if _, ok := got.Props["title"]; ok {
			t.Fatalf("reserved key title leaked into props: %#v", got.Props)
		}
	})

	t.Run("body frontmatter absorbed; body stored pure", func(t *testing.T) {
		p := mustCreate(fmt.Sprintf(
			`{"space_id":%d,"title":"P2","body":"---\nowner: cagdas\n---\n# Heading\n\ntext"}`, space))
		got := getPageHTTP(t, c, ts.URL, p.ID)
		if got.Body != "# Heading\n\ntext" {
			t.Fatalf("body = %q, want frontmatter stripped", got.Body)
		}
		if got.Props["owner"] != "cagdas" {
			t.Fatalf("props.owner = %v, want cagdas (absorbed from body)", got.Props["owner"])
		}
	})

	t.Run("explicit props field wins over body frontmatter", func(t *testing.T) {
		p := mustCreate(fmt.Sprintf(
			`{"space_id":%d,"title":"P3","body":"---\nfrom: body\n---\nx","props":{"from":"field"}}`, space))
		got := getPageHTTP(t, c, ts.URL, p.ID)
		if got.Props["from"] != "field" {
			t.Fatalf("props.from = %v, want field (explicit wins)", got.Props["from"])
		}
	})

	t.Run("update replaces the whole bag", func(t *testing.T) {
		p := mustCreate(fmt.Sprintf(
			`{"space_id":%d,"title":"P4","body":"b","props":{"a":"1","b":"2"}}`, space))
		resp, err := patchJSON(c, fmt.Sprintf("%s/api/pages/%d", ts.URL, p.ID), `{"props":{"a":"9"}}`)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("patch props: err=%v status=%d", err, resp.StatusCode)
		}
		resp.Body.Close()
		got := getPageHTTP(t, c, ts.URL, p.ID)
		if got.Props["a"] != "9" {
			t.Fatalf("props.a = %v, want 9", got.Props["a"])
		}
		if _, ok := got.Props["b"]; ok {
			t.Fatalf("Replace semantics violated: b survived: %#v", got.Props)
		}
	})

	t.Run("props-only update is allowed and leaves body unchanged", func(t *testing.T) {
		p := mustCreate(fmt.Sprintf(`{"space_id":%d,"title":"P5","body":"keep me","props":{}}`, space))
		resp, err := patchJSON(c, fmt.Sprintf("%s/api/pages/%d", ts.URL, p.ID), `{"props":{"k":"v"}}`)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("props-only patch: err=%v status=%d", err, resp.StatusCode)
		}
		resp.Body.Close()
		got := getPageHTTP(t, c, ts.URL, p.ID)
		if got.Props["k"] != "v" {
			t.Fatalf("props.k = %v, want v", got.Props["k"])
		}
		if got.Body != "keep me" {
			t.Fatalf("body changed on props-only update: %q", got.Body)
		}
	})
}

// TestSetPageProp exercises the bound-field write-back endpoint
// (PATCH /api/pages/{id}/props): the editor-only access gate, the server-side
// shallow-merge (one key can't clobber another — the property that separates this
// from the Replace-semantics PATCH /api/pages/{id}), typed values, and
// reserved-key rejection.
func TestSetPageProp(t *testing.T) {
	ts, d := newWiredServer(t)
	owner := seedUser(t, d, "propowner", "ownerpw12", false)
	viewer := seedUser(t, d, "propviewer", "viewerpw12", false)
	seedUser(t, d, "propstranger", "strangerpw12", false)
	space := seedSpace(t, d, "PropS", "props", owner)
	seedMember(t, d, space, viewer, "viewer")

	oc := loginClient(t, ts, "propowner", "ownerpw12")

	// A page to bind fields to, seeded with a starter field prop.
	resp, err := postJSON(oc, ts.URL+"/api/pages",
		fmt.Sprintf(`{"space_id":%d,"title":"UAT","body":"x","props":{"result":"pending"}}`, space))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("create page: err=%v status=%d", err, resp.StatusCode)
	}
	page := decodePage(t, resp)

	setProp := func(c *http.Client, body string) *http.Response {
		t.Helper()
		r, err := patchJSON(c, fmt.Sprintf("%s/api/pages/%d/props", ts.URL, page.ID), body)
		if err != nil {
			t.Fatalf("patch prop: %v", err)
		}
		return r
	}

	t.Run("owner flips a field; merge leaves other keys intact", func(t *testing.T) {
		r := setProp(oc, `{"key":"result","value":"pass"}`)
		if r.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(r.Body)
			r.Body.Close()
			t.Fatalf("set result: status=%d body=%s", r.StatusCode, b)
		}
		r.Body.Close()
		// A second flip of a DIFFERENT key must not clobber the first — this is
		// the merge (props || $1) semantics, not Replace.
		r2 := setProp(oc, `{"key":"notes","value":"looks good"}`)
		if r2.StatusCode != http.StatusOK {
			t.Fatalf("set notes: status=%d", r2.StatusCode)
		}
		r2.Body.Close()
		got := getPageHTTP(t, oc, ts.URL, page.ID)
		if got.Props["result"] != "pass" {
			t.Fatalf("props.result = %v, want pass (survives the second merge)", got.Props["result"])
		}
		if got.Props["notes"] != "looks good" {
			t.Fatalf("props.notes = %v, want 'looks good'", got.Props["notes"])
		}
	})

	t.Run("toggle writes a real JSON bool", func(t *testing.T) {
		r := setProp(oc, `{"key":"done","value":true}`)
		if r.StatusCode != http.StatusOK {
			t.Fatalf("set done: status=%d", r.StatusCode)
		}
		r.Body.Close()
		got := getPageHTTP(t, oc, ts.URL, page.ID)
		if got.Props["done"] != true {
			t.Fatalf("props.done = %#v, want JSON bool true", got.Props["done"])
		}
	})

	t.Run("viewer cannot write (403); the value is unchanged", func(t *testing.T) {
		vc := loginClient(t, ts, "propviewer", "viewerpw12")
		r := setProp(vc, `{"key":"result","value":"fail"}`)
		if r.StatusCode != http.StatusForbidden {
			t.Fatalf("viewer set: status=%d, want 403", r.StatusCode)
		}
		r.Body.Close()
		got := getPageHTTP(t, oc, ts.URL, page.ID)
		if got.Props["result"] != "pass" {
			t.Fatalf("viewer got 403 but props.result = %v, want unchanged pass", got.Props["result"])
		}
	})

	t.Run("non-member cannot write (403)", func(t *testing.T) {
		sc := loginClient(t, ts, "propstranger", "strangerpw12")
		r := setProp(sc, `{"key":"result","value":"fail"}`)
		if r.StatusCode != http.StatusForbidden {
			t.Fatalf("stranger set: status=%d, want 403", r.StatusCode)
		}
		r.Body.Close()
	})

	t.Run("reserved keys rejected (400)", func(t *testing.T) {
		for _, key := range []string{"id", "slug", "created"} {
			r := setProp(oc, fmt.Sprintf(`{"key":%q,"value":"x"}`, key))
			if r.StatusCode != http.StatusBadRequest {
				t.Fatalf("reserved key %q: status=%d, want 400", key, r.StatusCode)
			}
			r.Body.Close()
		}
	})

	t.Run("missing key rejected (400)", func(t *testing.T) {
		r := setProp(oc, `{"value":"x"}`)
		if r.StatusCode != http.StatusBadRequest {
			t.Fatalf("missing key: status=%d, want 400", r.StatusCode)
		}
		r.Body.Close()
	})
}
