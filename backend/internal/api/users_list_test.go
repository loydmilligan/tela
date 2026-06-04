package api
import ("encoding/json";"net/http";"testing")
func TestListUsers_Mention(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	_ = seedUser(t, d, "bob", "bobpw1234", false)
	_ = admin
	c := loginClient(t, ts, "admin", "adminpw12")
	resp, err := c.Get(ts.URL + "/api/users")
	if err != nil { t.Fatalf("get: %v", err) }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { t.Fatalf("status=%d", resp.StatusCode) }
	var body struct{ Users []struct{ ID int64 `json:"id"`; Username string `json:"username"` } `json:"users"` }
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil { t.Fatalf("decode: %v", err) }
	if len(body.Users) < 2 { t.Fatalf("expected >=2 users, got %d", len(body.Users)) }
	// unauth
	resp2, _ := http.Get(ts.URL + "/api/users")
	if resp2.StatusCode == http.StatusOK { t.Fatalf("unauth should not be 200") }
	resp2.Body.Close()
}
