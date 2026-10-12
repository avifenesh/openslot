package handler_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/calnode/calnode/internal/handler"
	"github.com/calnode/calnode/internal/inboxworker"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type inboxTransport func(*http.Request) (*http.Response, error)

func (f inboxTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func inboxCall(t *testing.T, h *handler.Handler, key, method, body string, fn http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.RequireAuth(fn)(w, authReq(method, "/v1/inbox", body, key))
	return w
}
func inboxSetup(t *testing.T) (*handler.Handler, *sql.DB, string) {
	t.Helper()
	h, db, key, user := setupWorkspaceWithDB(t)
	seedFullAvailabilityDB(t, db, user)
	w := inboxCall(t, h, key, "POST", `{"slug":"intro","name":"Intro","duration_minutes":30,"location_type":"phone","location_value":"+1 555 000 1111"}`, h.CreateEventType)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	h.SetMailer(nil, "https://schedule.example.test")
	return h, db, key
}
func inboxSave(t *testing.T, h *handler.Handler, key string, enabled bool) {
	t.Helper()
	w := inboxCall(t, h, key, "PUT", fmt.Sprintf(`{"address":"host@example.test","event_slug":"intro","timezone":"UTC","enabled":%t}`, enabled), h.SaveInboxSettings)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
}
func inboxClaim(t *testing.T, h *handler.Handler, key, engine string) string {
	t.Helper()
	w := inboxCall(t, h, key, "POST", `{"engine":"`+engine+`"}`, h.ClaimInbox)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var v struct {
		Token string `json:"token"`
	}
	json.Unmarshal(w.Body.Bytes(), &v)
	return v.Token
}
func inboxClient(t *testing.T, h *handler.Handler, token string) *mcp.ClientSession {
	t.Helper()
	s := httptest.NewServer(h.InboxMCPHandler())
	t.Cleanup(s.Close)
	client := &http.Client{Transport: inboxTransport(func(r *http.Request) (*http.Response, error) {
		r.Header.Set("Authorization", "Bearer "+token)
		return http.DefaultTransport.RoundTrip(r)
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	session, e := mcp.NewClient(&mcp.Implementation{Name: "inbox-test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: s.URL, HTTPClient: client, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { session.Close() })
	return session
}
func inboxTool(t *testing.T, c *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, e := c.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func inboxSlots(t *testing.T, c *mcp.ClientSession) []string {
	t.Helper()
	day := time.Now().UTC().AddDate(0, 0, 2).Format("2006-01-02")
	r := inboxTool(t, c, "find_email_slots", map[string]string{"from": day, "to": day})
	if r.IsError {
		t.Fatalf("slots error: %+v", r)
	}
	data, _ := json.Marshal(r.StructuredContent)
	var out struct {
		Slots []struct {
			Start string `json:"start"`
		} `json:"slots"`
	}
	if e := json.Unmarshal(data, &out); e != nil || len(out.Slots) < 2 {
		t.Fatalf("missing slots: %s", data)
	}
	return []string{out.Slots[0].Start, out.Slots[1].Start}
}
func TestInboxPreviewMCPLeaseAndRevalidation(t *testing.T) {
	h, db, key := inboxSetup(t)
	inboxSave(t, h, key, false)
	if w := inboxCall(t, h, key, "POST", "", h.PreviewInbox); w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	token := inboxClaim(t, h, key, "codex")
	c := inboxClient(t, h, token)
	list, e := c.ListTools(context.Background(), nil)
	if e != nil || len(list.Tools) != 4 {
		t.Fatal("must expose exactly four inbox tools", e)
	}
	if r := inboxTool(t, c, "read_email_request", map[string]any{}); r.IsError {
		t.Fatal(r)
	}
	starts := inboxSlots(t, c)
	// A fabricated slot must not complete the request.
	bad := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if r := inboxTool(t, c, "send_slot_offer", map[string]any{"starts": []string{bad}}); !r.IsError {
		t.Fatal("invented slot accepted")
	}
	// Change availability after discovery, proving delivery checks again.
	if _, e = db.Exec("UPDATE availability_rules SET start_time='23:58',end_time='23:59'"); e != nil {
		t.Fatal(e)
	}
	if r := inboxTool(t, c, "send_slot_offer", map[string]any{"starts": starts}); !r.IsError {
		t.Fatal("stale slots accepted")
	}
	db.Exec("UPDATE availability_rules SET start_time='00:00',end_time='23:59'")
	if r := inboxTool(t, c, "send_slot_offer", map[string]any{"starts": starts}); r.IsError {
		t.Fatalf("preview failed %+v", r)
	}
	var status, result string
	if e = db.QueryRow("SELECT status,result FROM inbox_requests").Scan(&status, &result); e != nil || status != "previewed" || !strings.Contains(result, "https://schedule.example.test/book/intro") {
		t.Fatal(status, result, e)
	}
	// Terminal leases cannot reconnect or execute a second action.
	req := httptest.NewRequest("POST", "/mcp/inbox", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.InboxMCPHandler().ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("completed lease remains valid", w.Code)
	}
}
func TestInboxAllEnginesClaimAndPause(t *testing.T) {
	h, db, key := inboxSetup(t)
	inboxSave(t, h, key, false)
	for _, engine := range []string{"codex", "claude", "webhook"} {
		if w := inboxCall(t, h, key, "POST", "", h.PreviewInbox); w.Code != 201 {
			t.Fatal(w.Code, w.Body)
		}
		token := inboxClaim(t, h, key, engine)
		c := inboxClient(t, h, token)
		if r := inboxTool(t, c, "ignore_email_request", map[string]any{}); r.IsError {
			t.Fatal(r)
		}
	}
	inboxCall(t, h, key, "POST", "", h.PreviewInbox)
	old := inboxClaim(t, h, key, "codex")
	db.Exec("UPDATE event_types SET is_public=0 WHERE slug='intro'")
	inboxSave(t, h, key, false)
	req := httptest.NewRequest("POST", "/mcp/inbox", nil)
	req.Header.Set("Authorization", "Bearer "+old)
	w := httptest.NewRecorder()
	h.InboxMCPHandler().ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("config change did not revoke lease")
	}
	var count int
	db.QueryRow("SELECT count(*) FROM inbox_requests WHERE status='cancelled'").Scan(&count)
	if count != 1 {
		t.Fatal("queued work not cancelled")
	}
}
func TestInboxGmailToCCDedupAndThreadedAutomaticOffer(t *testing.T) {
	h, db, key := inboxSetup(t)
	var sends atomic.Int32
	var rawMail string
	var badSend atomic.Bool
	h.SetGoogleAuth("test", "test", "https://schedule.example.test/callback", false)
	apps := h.InitAppConnectors(nil)
	apps.SetHTTPClient(&http.Client{Transport: inboxTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		status := 200
		switch {
		case strings.Contains(r.URL.Path, "token"):
			body = `{"access_token":"synthetic","refresh_token":"synthetic-refresh","token_type":"Bearer","expires_in":3600}`
		case strings.Contains(r.URL.Path, "userinfo"):
			body = `{"email":"host@example.test","email_verified":true}`
		case r.URL.Path == "/gmail/v1/users/me/messages":
			body = `{"messages":[{"id":"good"},{"id":"body-only"},{"id":"automatic"},{"id":"self"},{"id":"old"}]}`
		case r.URL.Path == "/gmail/v1/users/me/messages/send":
			sends.Add(1)
			var data map[string]string
			json.NewDecoder(r.Body).Decode(&data)
			if data["threadId"] != "thread-good" {
				t.Error("missing Gmail thread ID")
			}
			b, _ := base64.RawURLEncoding.DecodeString(data["raw"])
			rawMail = string(b)
			body = `{"id":"sent"}`
			if badSend.Load() {
				status = 503
			}
		default:
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			to := "someone@example.test"
			cc := "host@example.test"
			from := "guest@example.test"
			auto := "no"
			received := time.Now().UnixMilli()
			if id == "body-only" {
				cc = ""
			}
			if id == "automatic" {
				auto = "auto-replied"
			}
			if id == "self" {
				from = "host@example.test"
			}
			if id == "old" {
				received = time.Now().Add(-24 * time.Hour).UnixMilli()
			}
			headers := []map[string]string{{"name": "To", "value": to}, {"name": "Cc", "value": cc}, {"name": "From", "value": from}, {"name": "Subject", "value": "Meet next week"}, {"name": "Message-ID", "value": "<original@example.test>"}, {"name": "Auto-Submitted", "value": auto}}
			b, _ := json.Marshal(map[string]any{"id": id, "threadId": "thread-" + id, "internalDate": fmt.Sprint(received), "payload": map[string]any{"mimeType": "text/plain", "headers": headers, "body": map[string]string{"data": base64.RawURLEncoding.EncodeToString([]byte("Please schedule a meeting. Mention host@example.test in body only is not a trigger."))}}})
			body = string(b)
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	var user string
	db.QueryRow("SELECT id FROM users LIMIT 1").Scan(&user)
	if e := apps.ExchangeGmail(context.Background(), user, "code", "verifier"); e != nil {
		t.Fatal(e)
	}
	inboxSave(t, h, key, true)
	h.PollInbox(context.Background())
	h.PollInbox(context.Background())
	var n int
	db.QueryRow("SELECT COUNT(*) FROM inbox_requests").Scan(&n)
	if n != 1 {
		t.Fatalf("To/CC match, loops, activation guard or dedup failed: %d", n)
	}
	c := inboxClient(t, h, inboxClaim(t, h, key, "claude"))
	starts := inboxSlots(t, c)
	if r := inboxTool(t, c, "send_slot_offer", map[string]any{"starts": starts}); r.IsError {
		t.Fatal(r)
	}
	if sends.Load() != 1 || !strings.Contains(rawMail, "To: <guest@example.test>") || !strings.Contains(rawMail, "In-Reply-To: <original@example.test>") || !strings.Contains(rawMail, "Auto-Submitted: auto-replied") || strings.Contains(rawMail, "To: <someone@example.test>") {
		t.Fatalf("bad delivery count=%d mail=%s", sends.Load(), rawMail)
	}
	if e := apps.ExchangeGmail(context.Background(), user, "code", "verifier"); e != nil {
		t.Fatal(e)
	}
	h.PollInbox(context.Background())
	db.QueryRow("SELECT COUNT(*) FROM inbox_requests").Scan(&n)
	if n != 1 {
		t.Fatal("sent mail requeued")
	}
	// A failed send is terminal and not retried even when the lease expires.
	db.Exec("UPDATE inbox_requests SET status='pending',lease_until=0,gmail_generation=(SELECT generation FROM app_integrations WHERE provider='gmail')")
	badSend.Store(true)
	c2 := inboxClient(t, h, inboxClaim(t, h, key, "webhook"))
	if r := inboxTool(t, c2, "send_slot_offer", map[string]any{"starts": starts}); !r.IsError {
		t.Fatal("failed send reported success")
	}
	var state string
	db.QueryRow("SELECT status FROM inbox_requests").Scan(&state)
	if state != "uncertain" || sends.Load() != 2 {
		t.Fatal(state, sends.Load())
	}
	if w := inboxCall(t, h, key, "POST", `{"engine":"codex"}`, h.ClaimInbox); w.Code != 204 {
		t.Fatal("uncertain delivery automatically retried")
	}
}
func TestInboxWebhookWorkerCompletesThroughMCP(t *testing.T) {
	h, db, key := inboxSetup(t)
	inboxSave(t, h, key, false)
	inboxCall(t, h, key, "POST", "", h.PreviewInbox)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/inbox/claim", h.RequireAuth(h.ClaimInbox))
	mux.HandleFunc("GET /v1/inbox/requests/{id}", h.RequireAuth(h.InboxReceipt))
	mux.Handle("/mcp/inbox", h.InboxMCPHandler())
	server := httptest.NewServer(mux)
	defer server.Close()
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer webhook-test" {
			t.Error("missing webhook auth")
		}
		var task inboxworker.Task
		if json.NewDecoder(r.Body).Decode(&task) != nil {
			t.Error("invalid webhook body")
			w.WriteHeader(400)
			return
		}
		if task.Token == key || r.Header.Get("Idempotency-Key") != task.ID {
			t.Error("wrong task credentials")
		}
		c := inboxClient(t, h, task.Token)
		slots := inboxSlots(t, c)
		if v := inboxTool(t, c, "send_slot_offer", map[string]any{"starts": slots}); v.IsError {
			t.Error(v)
		}
		w.WriteHeader(202)
	}))
	defer hook.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if e := inboxworker.Run(ctx, inboxworker.Config{URL: server.URL, APIKey: key, Engine: "webhook", WebhookURL: hook.URL, WebhookToken: "webhook-test", Once: true}); e != nil {
		t.Fatal(e)
	}
	var status string
	db.QueryRow("SELECT status FROM inbox_requests").Scan(&status)
	if status != "previewed" {
		t.Fatal(status)
	}
}

func TestInboxInstalledAgentRuntime(t *testing.T) {
	engine := os.Getenv("OPENSLOT_TEST_ENGINE")
	if engine == "" {
		t.Skip("opt-in smoke of an installed authenticated CLI, using synthetic mail only")
	}
	if engine != "codex" && engine != "claude" {
		t.Fatal("unsupported test runtime")
	}
	h, db, key := inboxSetup(t)
	inboxSave(t, h, key, false)
	inboxCall(t, h, key, "POST", "", h.PreviewInbox)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/inbox/claim", h.RequireAuth(h.ClaimInbox))
	mux.HandleFunc("GET /v1/inbox/requests/{id}", h.RequireAuth(h.InboxReceipt))
	mux.Handle("/mcp/inbox", h.InboxMCPHandler())
	server := httptest.NewServer(mux)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	if e := inboxworker.Run(ctx, inboxworker.Config{URL: server.URL, APIKey: key, Engine: engine, Binary: os.Getenv("OPENSLOT_TEST_BINARY"), Once: true}); e != nil {
		t.Fatal(e)
	}
	var status string
	db.QueryRow("SELECT status FROM inbox_requests").Scan(&status)
	if status != "previewed" {
		t.Fatal("agent did not generate the sample offer:", status)
	}
}
