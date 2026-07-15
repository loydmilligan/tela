package api

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureNtfy is a fake ntfy server: it records every push POSTed to it (topic
// from the URL path + the ntfy headers/body), the way captureMailer captures
// email.
type captureNtfy struct {
	mu   sync.Mutex
	reqs []ntfyReq
	srv  *httptest.Server
}

type ntfyReq struct {
	Topic, Title, Body, Tags, Click, Auth string
}

func (c *captureNtfy) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.reqs)
}

func (c *captureNtfy) forTopic(topic string) (ntfyReq, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.reqs {
		if r.Topic == topic {
			return r, true
		}
	}
	return ntfyReq{}, false
}

// newNtfyServer builds a Server wired to a capturing fake ntfy endpoint.
func newNtfyServer(t *testing.T) (*Server, *sql.DB, *captureNtfy) {
	t.Helper()
	d := newAPITestDB(t)
	srv := New(d)
	cn := &captureNtfy{}
	cn.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cn.mu.Lock()
		cn.reqs = append(cn.reqs, ntfyReq{
			Topic: strings.TrimPrefix(r.URL.Path, "/"),
			Title: r.Header.Get("Title"),
			Body:  string(body),
			Tags:  r.Header.Get("Tags"),
			Click: r.Header.Get("Click"),
			Auth:  r.Header.Get("Authorization"),
		})
		cn.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(cn.srv.Close)
	srv.ntfy = ntfyConfig{URL: cn.srv.URL, Client: cn.srv.Client()}
	return srv, d, cn
}

func setNtfyTopic(t *testing.T, d *sql.DB, userID int64, topic string) {
	t.Helper()
	if _, err := d.ExecContext(context.Background(),
		`UPDATE users SET ntfy_topic = $1 WHERE id = $2`, topic, userID); err != nil {
		t.Fatalf("set ntfy topic: %v", err)
	}
}

func waitForNtfy(t *testing.T, cn *captureNtfy, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cn.count() >= want {
			time.Sleep(60 * time.Millisecond)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d ntfy pushes, got %d", want, cn.count())
}

func assertNoNtfy(t *testing.T, cn *captureNtfy, baseline int) {
	t.Helper()
	time.Sleep(250 * time.Millisecond)
	if got := cn.count(); got != baseline {
		t.Fatalf("expected no new ntfy push (baseline %d), got %d", baseline, got)
	}
}

func TestNotificationNtfy_Delivery(t *testing.T) {
	srv, d, cn := newNtfyServer(t)
	ctx := context.Background()
	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	setNtfyTopic(t, d, bob, "bob-tela-abc")
	spaceID := seedSpace(t, d, "Engineering", "engineering", alice)
	seedMember(t, d, spaceID, bob, "editor")

	body := "Rollout plan — [@bob](tela://user/" + intStr(bob) + ") please review."
	srv.notifyPageMentions(ctx, authUser(alice, "alice", false), 42, spaceID, "Rollout", body)

	waitForNtfy(t, cn, 1)
	req, ok := cn.forTopic("bob-tela-abc")
	if !ok {
		t.Fatalf("no push to bob's topic; got %d", cn.count())
	}
	if !strings.Contains(req.Title, "alice") || !strings.Contains(req.Title, "Rollout") {
		t.Errorf("title missing actor/page: %q", req.Title)
	}
	if !strings.Contains(req.Click, "/pages/42") {
		t.Errorf("Click header should deep-link the page: %q", req.Click)
	}
	if req.Tags != "speech_balloon" {
		t.Errorf("mention tag = %q, want speech_balloon", req.Tags)
	}
	if !strings.Contains(req.Body, "please review") {
		t.Errorf("snippet not in body: %q", req.Body)
	}
}

func TestNotificationNtfy_TokenHeader(t *testing.T) {
	srv, d, cn := newNtfyServer(t)
	srv.ntfy.Token = "tk_secret" // protected topic
	ctx := context.Background()
	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	setNtfyTopic(t, d, bob, "bob-secure")
	spaceID := seedSpace(t, d, "Eng", "eng", alice)
	seedMember(t, d, spaceID, bob, "editor")

	srv.notifyPageMentions(ctx, authUser(alice, "alice", false), 7, spaceID, "Plan",
		"[@bob](tela://user/"+intStr(bob)+")")
	waitForNtfy(t, cn, 1)
	req, _ := cn.forTopic("bob-secure")
	if req.Auth != "Bearer tk_secret" {
		t.Errorf("Authorization header = %q, want Bearer tk_secret", req.Auth)
	}
}

func TestNotificationNtfy_PrefGating(t *testing.T) {
	srv, d, cn := newNtfyServer(t)
	ctx := context.Background()
	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	setNtfyTopic(t, d, bob, "bob-t")
	spaceID := seedSpace(t, d, "Eng", "eng", alice)
	seedMember(t, d, spaceID, bob, "editor")
	// Bob mutes the ntfy channel for mentions (in-app + email untouched).
	if _, err := d.ExecContext(ctx,
		`INSERT INTO notification_prefs (user_id, event_type, channel, enabled) VALUES ($1, 'mention', 'ntfy', 0)`,
		bob); err != nil {
		t.Fatalf("insert pref: %v", err)
	}

	srv.notifyPageMentions(ctx, authUser(alice, "alice", false), 9, spaceID, "Notes",
		"[@bob](tela://user/"+intStr(bob)+") fyi")

	assertNoNtfy(t, cn, 0)
	// Muting ntfy must not mute the inbox.
	if n := notifCountByType(t, d, bob, notifMention); n != 1 {
		t.Fatalf("bob in-app mention = %d, want 1 (ntfy opt-out must not affect in-app)", n)
	}
}

func TestNotificationNtfy_TopicUnsetSkips(t *testing.T) {
	srv, d, cn := newNtfyServer(t)
	ctx := context.Background()
	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false) // no ntfy_topic (default '')
	spaceID := seedSpace(t, d, "Eng", "eng", alice)
	seedMember(t, d, spaceID, bob, "editor")

	srv.notifyPageMentions(ctx, authUser(alice, "alice", false), 11, spaceID, "Notes",
		"[@bob](tela://user/"+intStr(bob)+") hi")

	assertNoNtfy(t, cn, 0)
	if n := notifCountByType(t, d, bob, notifMention); n != 1 {
		t.Fatalf("bob in-app mention = %d, want 1 (no topic must not affect in-app)", n)
	}
}

func TestNotificationNtfy_ChannelInert(t *testing.T) {
	srv, d, cn := newNtfyServer(t)
	srv.ntfy = ntfyConfig{URL: ""} // TELA_NTFY_URL unset → channel inert
	ctx := context.Background()
	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	setNtfyTopic(t, d, bob, "bob-t") // topic set, but channel is off entirely
	spaceID := seedSpace(t, d, "Eng", "eng", alice)
	seedMember(t, d, spaceID, bob, "editor")

	srv.notifyPageMentions(ctx, authUser(alice, "alice", false), 13, spaceID, "Notes",
		"[@bob](tela://user/"+intStr(bob)+") hey")

	assertNoNtfy(t, cn, 0) // the capture server is never hit
	if n := notifCountByType(t, d, bob, notifMention); n != 1 {
		t.Fatalf("bob in-app mention = %d, want 1 (inert ntfy must not affect in-app)", n)
	}
}

func TestNotificationNtfy_PageUpdatedThrottled(t *testing.T) {
	srv, d, cn := newNtfyServer(t)
	ctx := context.Background()
	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	setNtfyTopic(t, d, bob, "bob-page")
	spaceID := seedSpace(t, d, "Eng", "eng", alice)
	seedMember(t, d, spaceID, bob, "editor")
	pageID := int64(202)
	if _, err := d.ExecContext(ctx,
		`INSERT INTO subscriptions (user_id, subject_kind, subject_id) VALUES ($1, 'page', $2)`,
		bob, pageID); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	oldBody := "Intro.\nStep one: provision.\nStep two: migrate."
	newBody := "Intro.\nStep one: provision the cluster.\nStep two: migrate.\nStep three: verify."
	// Two edits in quick succession → only ONE push (throttled per page/window).
	srv.notifyPageUpdate(ctx, authUser(alice, "alice", false), pageID, spaceID, "Roadmap", oldBody, newBody)
	waitForNtfy(t, cn, 1)
	req, ok := cn.forTopic("bob-page")
	if !ok || !strings.Contains(req.Title, "Roadmap") {
		t.Fatalf("page_updated push missing/title absent: %+v ok=%v", req, ok)
	}
	if req.Tags != "pencil2" {
		t.Errorf("page_updated tag = %q, want pencil2", req.Tags)
	}
	srv.notifyPageUpdate(ctx, authUser(alice, "alice", false), pageID, spaceID, "Roadmap", oldBody, newBody)
	assertNoNtfy(t, cn, 1)
}
