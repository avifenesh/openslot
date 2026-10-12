package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/calnode/calnode/internal/config"
	"github.com/calnode/calnode/internal/db"
	"github.com/calnode/calnode/internal/handler"
)

func TestAgentAndAppConnectorRoutes(t *testing.T) {
	database, err := db.Open("sqlite://:memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err = db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	seed := handler.New(database, logger)
	recorder := httptest.NewRecorder()
	seed.Setup(recorder, httptest.NewRequest("POST", "/v1/setup", strings.NewReader(`{"name":"Host","email":"host@example.test","timezone":"UTC"}`)))
	var identity struct {
		APIKey string `json:"api_key"`
	}
	if err = json.Unmarshal(recorder.Body.Bytes(), &identity); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	app, drain := New(ctx, &config.Config{BaseURL: "http://localhost:3000", DataDir: t.TempDir(), GoogleClientID: "test-client.apps.googleusercontent.com", GoogleClientSecret: "test-secret"}, database, logger)
	t.Cleanup(func() { cancel(); drain() })
	call := func(method, path, body, key string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/v1/agent-connectors", "/v1/apps"} {
		if got := call("GET", path, "", ""); got.Code != 401 {
			t.Fatalf("unauthorized %s: %d", path, got.Code)
		}
	}
	catalog := call("GET", "/v1/agent-connectors", "", identity.APIKey)
	if catalog.Code != 200 {
		t.Fatalf("connector catalog: %d %s", catalog.Code, catalog.Body)
	}
	for _, name := range []string{"codex", "antigravity", "openai", "anthropic", "bedrock", "glm", "grok"} {
		if !strings.Contains(catalog.Body.String(), `"id":"`+name+`"`) {
			t.Errorf("missing %s", name)
		}
	}
	invalid := call("POST", "/v1/api-keys", `{"name":"bad","connector_id":"invented"}`, identity.APIKey)
	if invalid.Code != 400 {
		t.Fatalf("unknown connector accepted: %d", invalid.Code)
	}
	created := call("POST", "/v1/api-keys", `{"name":"Codex","connector_id":"codex"}`, identity.APIKey)
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	if created.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("key creation must not be cached")
	}
	var key struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	json.Unmarshal(created.Body.Bytes(), &key)
	list := call("GET", "/v1/api-keys", "", identity.APIKey)
	if !strings.Contains(list.Body.String(), `"connector_id":"codex"`) || strings.Contains(list.Body.String(), key.Key) {
		t.Fatal("key metadata missing or plaintext leaked")
	}
	apps := call("GET", "/v1/apps", "", identity.APIKey)
	if apps.Code != 200 || !strings.Contains(apps.Body.String(), `"gmail_oauth_configured":true`) {
		t.Fatalf("apps: %d %s", apps.Code, apps.Body)
	}
	if _, err = database.Exec("INSERT INTO users(id,email,name) VALUES('member','member@example.test','Member')"); err != nil {
		t.Fatal(err)
	}
	memberHash := sha256.Sum256([]byte("member-test-key"))
	if _, err = database.Exec("INSERT INTO api_keys(id,user_id,name,key_hash) VALUES('member-key','member','Member',?)", fmt.Sprintf("%x", memberHash)); err != nil {
		t.Fatal(err)
	}
	for _, route := range [][2]string{{"GET", "/v1/apps"}, {"GET", "/v1/apps/gmail/connect"}, {"PUT", "/v1/apps/notion"}, {"POST", "/v1/apps/gmail/check"}, {"DELETE", "/v1/apps/notion"}, {"POST", "/v1/apps/notion/sync"}} {
		if got := call(route[0], route[1], "{}", "member-test-key"); got.Code != 403 {
			t.Fatalf("member app access %s: %d", route[1], got.Code)
		}
	}
	if got := call("GET", "/v1/agent-connectors", "", "member-test-key"); got.Code != 200 {
		t.Fatal("member cannot configure own agent")
	}
	if got := call("DELETE", "/v1/api-keys/"+key.ID, "", "member-test-key"); got.Code != 404 {
		t.Fatal("member can revoke another user's key")
	}
	connect := call("GET", "/v1/apps/gmail/connect", "", identity.APIKey)
	if connect.Code != 302 {
		t.Fatalf("gmail start: %d %s", connect.Code, connect.Body)
	}
	location, err := url.Parse(connect.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	query := location.Query()
	if query.Get("code_challenge_method") != "S256" || !strings.Contains(query.Get("scope"), "gmail.send") || !strings.Contains(query.Get("scope"), "gmail.readonly") {
		t.Fatal("wrong Gmail consent/PKCE")
	}
	if query.Get("redirect_uri") != "http://localhost:3000/v1/apps/gmail/callback" {
		t.Fatal("wrong Gmail redirect")
	}
	cookies := connect.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly {
		t.Fatal("state cookie missing")
	}
	deniedPath := "/v1/apps/gmail/callback?error=access_denied&state=" + url.QueryEscape(query.Get("state"))
	if wrong := call("GET", deniedPath, "", identity.APIKey); wrong.Code != 400 {
		t.Fatal("state accepted without cookie")
	}
	if _, err = database.Exec("UPDATE users SET is_admin=1 WHERE id='member'"); err != nil {
		t.Fatal(err)
	}
	if other := call("GET", deniedPath, "", "member-test-key", cookies...); other.Code != 400 {
		t.Fatal("OAuth state accepted for a different signed-in administrator")
	}
	if denied := call("GET", deniedPath, "", identity.APIKey, cookies...); denied.Code != 303 {
		t.Fatalf("denial: %d", denied.Code)
	}
	if replay := call("GET", deniedPath, "", identity.APIKey, cookies...); replay.Code != 400 {
		t.Fatal("OAuth state replay accepted")
	}
	if result := call("DELETE", "/v1/api-keys/"+key.ID, "", identity.APIKey); result.Code != 204 {
		t.Fatal("revoke failed")
	}
	if result := call("GET", "/v1/agent-connectors", "", key.Key); result.Code != 401 {
		t.Fatal("revoked key accepted")
	}
}

func TestInvalidGoogleConfigurationDoesNotOfferSignIn(t *testing.T) {
	for _, tc := range []struct{ name, id, secret string }{
		{"malformed ID", "project-id", "secret"},
		{"missing secret", "valid.apps.googleusercontent.com", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, err := db.Open("sqlite://:memory:")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { database.Close() })
			if err := db.Migrate(database); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			app, drain := New(ctx, &config.Config{BaseURL: "http://localhost:3000", DataDir: t.TempDir(), GoogleClientID: tc.id, GoogleClientSecret: tc.secret}, database, slog.New(slog.NewTextHandler(io.Discard, nil)))
			t.Cleanup(func() { cancel(); drain() })
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/auth/status", nil))
			var state struct {
				Providers []string `json:"providers"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
				t.Fatal(err)
			}
			for _, p := range state.Providers {
				if p == "google" {
					t.Fatal("invalid credentials offered as Google sign-in")
				}
			}
			rec = httptest.NewRecorder()
			app.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/auth/login", nil))
			if rec.Code != 503 || rec.Header().Get("Location") != "" {
				t.Fatal("invalid credentials redirected to Google")
			}
		})
	}
}

func TestSessionCookieSecurityDoesNotDependOnGoogleReadiness(t *testing.T) {
	for _, tc := range []struct {
		name, id, secret string
		secure           bool
	}{
		{"malformed Google on HTTPS", "project-id", "secret", true},
		{"missing Google secret on HTTPS", "valid.apps.googleusercontent.com", "", true},
		{"local HTTP control", "project-id", "secret", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, err := db.Open("sqlite://:memory:")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { database.Close() })
			if err := db.Migrate(database); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			app, drain := New(ctx, &config.Config{BaseURL: "https://schedule.example.test", DataDir: t.TempDir(), CookieSecure: tc.secure, GoogleClientID: tc.id, GoogleClientSecret: tc.secret}, database, slog.New(slog.NewTextHandler(io.Discard, nil)))
			t.Cleanup(func() { cancel(); drain() })
			claim := httptest.NewRecorder()
			app.ServeHTTP(claim, httptest.NewRequest("POST", "/v1/auth/claim", strings.NewReader(`{"name":"Owner","email":"owner@example.test","password":"Synthetic-password-2026","timezone":"UTC"}`)))
			if claim.Code >= 300 {
				t.Fatalf("claim: %d %s", claim.Code, claim.Body.String())
			}
			login := httptest.NewRecorder()
			app.ServeHTTP(login, httptest.NewRequest("POST", "/v1/auth/login/email", strings.NewReader(`{"email":"owner@example.test","password":"Synthetic-password-2026"}`)))
			if login.Code != 200 {
				t.Fatalf("login: %d %s", login.Code, login.Body.String())
			}
			for _, cookie := range login.Result().Cookies() {
				if cookie.Name == "calnode_session" {
					if cookie.Secure != tc.secure {
						t.Fatalf("session Secure=%v, want %v", cookie.Secure, tc.secure)
					}
					return
				}
			}
			t.Fatal("session cookie missing")
		})
	}
}
