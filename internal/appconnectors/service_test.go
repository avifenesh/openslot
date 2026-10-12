package appconnectors

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/calnode/calnode/internal/db"
	"github.com/calnode/calnode/internal/mailer"
	"github.com/calnode/calnode/internal/secret"
	"golang.org/x/oauth2"
	"golang.org/x/time/rate"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fallbackMailer struct{ sent int }

func (f *fallbackMailer) Send(context.Context, mailer.Message) error { f.sent++; return nil }

func setupService(t *testing.T, serve http.HandlerFunc) (*Service, *sql.DB, *fallbackMailer) {
	t.Helper()
	database, err := db.Open("sqlite://:memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err = db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec("INSERT INTO users(id,email,name,is_admin) VALUES('host','host@example.test','Host',1)"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(serve)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	client := &http.Client{Timeout: time.Second * 5, Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		u := *r.URL
		u.Scheme = target.Scheme
		u.Host = target.Host
		copy.URL = &u
		return http.DefaultTransport.RoundTrip(copy)
	})}
	fallback := &fallbackMailer{}
	service := New(database, [32]byte{1, 2, 3}, func() *oauth2.Config {
		return &oauth2.Config{ClientID: "test", ClientSecret: "test-secret", RedirectURL: "https://scheduler.example.test/v1/apps/gmail/callback", Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/auth", TokenURL: "https://oauth2.googleapis.com/token", AuthStyle: oauth2.AuthStyleInParams}}
	}, mailer.NewLive(fallback))
	service.SetHTTPClient(client)
	service.notionRate = rate.NewLimiter(rate.Inf, 1)
	return service, database, fallback
}

func TestGmailOAuthRefreshMIMEAndDisconnect(t *testing.T) {
	sends, refreshes := 0, 0
	sendStatus := 200
	s, database, fallback := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			r.ParseForm()
			if r.Form.Get("grant_type") == "authorization_code" && r.Form.Get("code_verifier") != "verifier" {
				t.Error("missing PKCE verifier")
			}
			if r.Form.Get("grant_type") == "refresh_token" {
				refreshes++
				io.WriteString(w, `{"access_token":"refreshed-access","token_type":"Bearer","expires_in":3600}`)
			} else {
				io.WriteString(w, `{"access_token":"initial-access","refresh_token":"offline-refresh","token_type":"Bearer","expires_in":3600,"scope":"openid email https://www.googleapis.com/auth/gmail.send"}`)
			}
		case "/v1/userinfo":
			io.WriteString(w, `{"email":"host@example.test","email_verified":true,"name":"Host"}`)
		case "/gmail/v1/users/me/messages/send":
			sends++
			if sendStatus != 200 {
				w.WriteHeader(sendStatus)
				io.WriteString(w, `{"error":"provider text must not be logged"}`)
				return
			}
			var body struct {
				Raw string `json:"raw"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			raw, err := base64.RawURLEncoding.DecodeString(body.Raw)
			if err != nil {
				t.Error(err)
			}
			message, err := mail.ReadMessage(strings.NewReader(string(raw)))
			if err != nil {
				t.Error(err)
				return
			}
			from, err := mail.ParseAddress(message.Header.Get("From"))
			if err != nil || from.Address != "host@example.test" {
				t.Error("incorrect Gmail sender")
			}
			if !strings.Contains(string(raw), "invite.ics") || !strings.Contains(message.Header.Get("Content-Type"), "multipart/mixed") {
				t.Error("calendar attachment not preserved")
			}
			io.WriteString(w, `{"id":"gmail-message"}`)
		default:
			t.Errorf("unexpected provider path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	ctx := t.Context()
	msg := mailer.Message{To: []string{"guest@example.test"}, Subject: "Meeting", Text: "Confirmed", HTML: "<p>Confirmed</p>", Attachments: []mailer.Attachment{{Filename: "invite.ics", ContentType: "text/calendar", Content: []byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR")}}}
	if err := s.Send(ctx, msg); err != nil || fallback.sent != 1 {
		t.Fatal("unconnected sender must use configured fallback")
	}
	if err := s.ExchangeGmail(ctx, "host", "code", "verifier"); err != nil {
		t.Fatal(err)
	}
	if !s.EmailEnabled() {
		t.Fatal("Gmail not enabled")
	}
	var encrypted string
	if err := database.QueryRow("SELECT credentials_enc FROM app_integrations WHERE provider='gmail'").Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, "offline-refresh") || strings.Contains(encrypted, "initial-access") {
		t.Fatal("plaintext credentials stored")
	}
	plain, err := secret.Decrypt(s.key, encrypted)
	if err != nil {
		t.Fatal(err)
	}
	var token oauth2.Token
	json.Unmarshal([]byte(plain), &token)
	token.Expiry = time.Now().Add(-time.Hour)
	raw, _ := json.Marshal(token)
	expired, _ := secret.Encrypt(s.key, string(raw))
	database.Exec("UPDATE app_integrations SET credentials_enc=? WHERE provider='gmail'", expired)
	if err = s.CheckGmail(ctx); err != nil {
		t.Fatal(err)
	}
	if sends != 0 || refreshes != 1 {
		t.Fatal("connection check sent mail or failed to refresh")
	}
	if err = s.Send(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if sends != 1 || refreshes != 1 {
		t.Fatal("refreshed token was not persisted")
	}
	status, err := s.Status(ctx, "gmail")
	if err != nil || !status.Connected || status.LastSuccess == nil {
		t.Fatal("missing connection receipt")
	}
	visible, _ := json.Marshal(status)
	if strings.Contains(string(visible), "access") || strings.Contains(string(visible), "refresh") {
		t.Fatal("status leaks credentials")
	}
	sendStatus = 429
	if err = s.Send(ctx, msg); err == nil || strings.Contains(err.Error(), "provider text") {
		t.Fatal("provider failure missing or unsanitized")
	}
	if fallback.sent != 1 {
		t.Fatal("provider failure silently changed sender")
	}
	if err = s.Disconnect(ctx, "gmail"); err != nil {
		t.Fatal(err)
	}
	if err = s.Send(ctx, msg); err != nil || fallback.sent != 2 {
		t.Fatal("disconnect did not restore configured sender")
	}
}

func TestGmailPartialConsentDoesNotConnect(t *testing.T) {
	s, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"partial","refresh_token":"partial-refresh","token_type":"Bearer","scope":"openid email"}`)
	})
	if err := s.ExchangeGmail(t.Context(), "host", "code", "verifier"); err == nil {
		t.Fatal("partial consent accepted")
	}
	status, _ := s.Status(t.Context(), "gmail")
	if status.Connected {
		t.Fatal("partial credentials saved")
	}
}

func TestNotionBookingLifecycleAndRetry(t *testing.T) {
	created, patched := 0, 0
	var remoteID string
	var remoteProps map[string]any
	s, database, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Notion-Version") != "2025-09-03" || r.Header.Get("Authorization") != "Bearer notion-test-token" {
			t.Error("Notion protocol/auth mismatch")
		}
		switch {
		case r.URL.Path == "/v1/users/me":
			io.WriteString(w, `{"id":"notion-bot"}`)
		case r.Method == "GET" && r.URL.Path == "/v1/pages/01234567-89ab-cdef-0123-456789abcdef":
			io.WriteString(w, `{"id":"parent"}`)
		case r.Method == "POST" && r.URL.Path == "/v1/databases":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			source, ok := body["initial_data_source"].(map[string]any)
			if !ok || source["properties"] == nil {
				t.Error("missing database schema")
			}
			io.WriteString(w, `{"id":"database","url":"https://www.notion.so/database","data_sources":[{"id":"source"}]}`)
		case r.Method == "GET" && r.URL.Path == "/v1/data_sources/source":
			io.WriteString(w, `{"properties":{"Name":{"type":"title"},"OpenSlot ID":{"type":"rich_text"},"Meeting":{"type":"rich_text"},"Guests":{"type":"rich_text"},"Time":{"type":"date"},"Status":{"type":"select"}}}`)
		case r.URL.Path == "/v1/data_sources/source/query":
			if remoteID == "" {
				io.WriteString(w, `{"results":[]}`)
			} else {
				io.WriteString(w, `{"results":[{"id":"page-1"}]}`)
			}
		case r.URL.Path == "/v1/pages" && r.Method == "POST":
			created++
			remoteID = "page-1"
			var body struct {
				Properties map[string]any `json:"properties"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			remoteProps = body.Properties
			io.WriteString(w, `{"id":"page-1"}`)
		case r.URL.Path == "/v1/pages/page-1" && r.Method == "PATCH":
			patched++
			var body struct {
				Properties map[string]any `json:"properties"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			remoteProps = body.Properties
			io.WriteString(w, `{"id":"page-1"}`)
		default:
			t.Errorf("unexpected Notion path %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	})
	ctx := t.Context()
	if err := s.ConnectNotion(ctx, "host", "notion-test-token", "https://www.notion.so/Calendar-0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckNotion(ctx); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		"INSERT INTO event_types(id,user_id,slug,name,duration_minutes) VALUES('event','host','intro','Intro call',30)",
		"INSERT INTO bookings(id,event_type_id,host_id,start_at,end_at,payment_status) VALUES('booking','event','host','2026-11-01 09:00:00','2026-11-01 09:30:00','pending')",
		"INSERT INTO booking_attendees(id,booking_id,name,email) VALUES('guest','booking','Guest','guest@example.test')",
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	var payload string
	if err := database.QueryRow("SELECT payload FROM jobs WHERE type='notion.sync'").Scan(&payload); err != nil {
		t.Fatal("booking did not queue Notion sync:", err)
	}
	if err := s.SyncNotion(ctx, payload); err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatal("booking was not exported")
	}
	if remoteProps["Status"].(map[string]any)["select"].(map[string]any)["name"] != "pending_payment" {
		t.Fatal("unpaid booking exported as confirmed")
	}
	timeProp := remoteProps["Time"].(map[string]any)["date"].(map[string]any)
	if timeProp["start"] != "2026-11-01T09:00:00Z" {
		t.Fatal("incorrect Notion date")
	}
	database.Exec("DELETE FROM notion_booking_pages") // Remote write succeeded but local receipt was lost.
	if err := s.SyncNotion(ctx, payload); err != nil {
		t.Fatal(err)
	}
	if created != 1 || patched != 1 {
		t.Fatal("retry duplicated the remote page")
	}
	if _, err := database.Exec("UPDATE bookings SET payment_status='paid' WHERE id='booking'"); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncNotion(ctx, payload); err != nil {
		t.Fatal(err)
	}
	if remoteProps["Status"].(map[string]any)["select"].(map[string]any)["name"] != "confirmed" {
		t.Fatal("payment completion not reflected")
	}
	if _, err := database.Exec("UPDATE bookings SET status='cancelled' WHERE id='booking'"); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncNotion(ctx, payload); err != nil {
		t.Fatal(err)
	}
	if remoteProps["Status"].(map[string]any)["select"].(map[string]any)["name"] != "cancelled" {
		t.Fatal("delayed job restored stale state")
	}
	var jobs int
	database.QueryRow("SELECT COUNT(*) FROM jobs WHERE type='notion.sync'").Scan(&jobs)
	if jobs != 3 {
		t.Fatalf("create, payment and cancellation should queue three jobs, got %d", jobs)
	}
	count, err := s.QueueNotionBackfill(ctx)
	if err != nil || count != 1 {
		t.Fatal("backfill did not enqueue existing booking")
	}
	if err = s.Disconnect(ctx, "notion"); err != nil {
		t.Fatal(err)
	}
	previous := patched
	if err = s.SyncNotion(ctx, payload); err != nil || patched != previous {
		t.Fatal("disconnected integration still delivered")
	}
}

type concurrentFallback struct {
	started chan struct{}
	release chan struct{}
}

func (f *concurrentFallback) Send(ctx context.Context, _ mailer.Message) error {
	f.started <- struct{}{}
	select {
	case <-f.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestGmailFallbackDoesNotSerializeAndGateCanCancel(t *testing.T) {
	s, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected provider call"); w.WriteHeader(500) })
	blocked := &concurrentFallback{started: make(chan struct{}, 2), release: make(chan struct{})}
	s.fallback.Swap(blocked)
	done := make(chan error, 2)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	defer close(blocked.release)
	for i := 0; i < 2; i++ {
		go func() { done <- s.Send(ctx, mailer.Message{}) }()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-blocked.started:
		case <-ctx.Done():
			t.Fatal("unconnected Gmail serialized independent fallback deliveries")
		}
	}
	cancel()
	for i := 0; i < 2; i++ {
		<-done
	}
	s.gmailActive.Store(true)
	if err := s.acquire(t.Context(), "gmail"); err != nil {
		t.Fatal(err)
	}
	defer s.release("gmail")
	canceled, stop := context.WithCancel(t.Context())
	stop()
	answer := make(chan error, 1)
	go func() { answer <- s.Send(canceled, mailer.Message{}) }()
	select {
	case err := <-answer:
		if err != context.Canceled {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Gmail gate ignored caller cancellation")
	}
}

func TestNotionRetryAfterPersistsAndDoesNotCallEarly(t *testing.T) {
	calls := 0
	s, database, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "180")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if err := s.save(t.Context(), "notion", "host", "test-token", Config{DataSourceID: "source"}); err != nil {
		t.Fatal(err)
	}
	var retry providerRetry
	err := s.CheckNotion(t.Context())
	if !errors.As(err, &retry) || !retry.RequestAttempted() || time.Until(retry.RetryAt()) < 179*time.Second {
		t.Fatal("provider retry deadline not recorded")
	}
	err = s.CheckNotion(t.Context())
	if !errors.As(err, &retry) || retry.RequestAttempted() || calls != 1 {
		t.Fatal("request sent during provider cooldown")
	}
	restarted := New(database, s.key, nil, s.fallback)
	restarted.SetHTTPClient(s.client)
	err = restarted.CheckNotion(t.Context())
	if !errors.As(err, &retry) || retry.RequestAttempted() || calls != 1 {
		t.Fatal("provider cooldown lost across restart")
	}
}
