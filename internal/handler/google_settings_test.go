package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/calnode/calnode/internal/caldav"
	"github.com/calnode/calnode/internal/calendar"
	"github.com/calnode/calnode/internal/calendar/microsoft"
	"github.com/calnode/calnode/internal/handler"
	"github.com/calnode/calnode/internal/mailer"
)

// newGoogleHandler sets up a handler with a valid enc key and returns it
// alongside the DB and API key for an admin user.
func newGoogleHandler(t *testing.T) (*handler.Handler, *sql.DB, string) {
	t.Helper()
	h, database, apiKey, _ := setupWorkspaceWithDB(t)
	// Use the same key as calendar tests so gcal.New works if needed.
	h.SetEncKey(testGCalKeyHex)
	h.SetBaseURL("http://localhost:3000")
	return h, database, apiKey
}

// getGoogleSettings calls GET /v1/settings/google and returns the recorder.
func getGoogleSettings(t *testing.T, h *handler.Handler, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	req := authReq(http.MethodGet, "/v1/settings/google", "", apiKey)
	rec := httptest.NewRecorder()
	h.RequireAuth(h.GetGoogleSettings)(rec, req)
	return rec
}

// patchGoogleSettings calls PATCH /v1/settings/google and returns the recorder.
func patchGoogleSettings(t *testing.T, h *handler.Handler, body, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	req := authReq(http.MethodPatch, "/v1/settings/google", body, apiKey)
	rec := httptest.NewRecorder()
	h.RequireAuth(h.PatchGoogleSettings)(rec, req)
	return rec
}

// ---------------------------------------------------------------------------
// GET /v1/settings/google
// ---------------------------------------------------------------------------

func TestGoogleDesktopClientImport(t *testing.T) {
	h, database, key := newGoogleHandler(t)
	body := `{"client_id":"own-desktop.apps.googleusercontent.com","client_secret":"synthetic-desktop-secret","client_type":"desktop"}`
	if rec := patchGoogleSettings(t, h, body, key); rec.Code != 400 {
		t.Fatalf("hosted server accepted desktop client: %d", rec.Code)
	}
	h.SetGoogleDesktopAllowed(true)
	rec := patchGoogleSettings(t, h, body, key)
	if rec.Code != 200 {
		t.Fatalf("desktop import: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "synthetic-desktop-secret") {
		t.Fatal("response exposed client secret")
	}
	saved, err := handler.LoadGoogleSettingsFromDB(database, [32]byte{0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef})
	if err != nil || saved.ClientType != "desktop" || saved.ClientSecret != "synthetic-desktop-secret" {
		t.Fatalf("persisted desktop client invalid: %v", err)
	}
	h.SetGoogleDesktopAllowed(false)
	var status map[string]any
	json.Unmarshal(getGoogleSettings(t, h, key).Body.Bytes(), &status)
	if status["configured"] != false {
		t.Fatal("moved desktop installation still claims ready on hosted server")
	}
}

func TestGoogleAppReplacementPreservesConnectedCalendar(t *testing.T) {
	h, database, key := newGoogleHandler(t)
	original := `{"client_id":"original.apps.googleusercontent.com","client_secret":"original-fixture-secret"}`
	if got := patchGoogleSettings(t, h, original, key); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	_, err := database.Exec(`INSERT INTO calendar_connections(id,user_id,provider,access_token_enc,calendar_id) SELECT 'fixture-google-connection',id,'google','encrypted-fixture','primary' FROM users LIMIT 1`)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"client_id":"replacement.apps.googleusercontent.com","client_secret":"new-fixture-secret"}`, `{"client_id":""}`} {
		got := patchGoogleSettings(t, h, body, key)
		if got.Code != http.StatusConflict {
			t.Fatalf("expected disconnect instruction, got %d: %s", got.Code, got.Body.String())
		}
	}
	var clientID string
	if err := database.QueryRow("SELECT google_client_id FROM server_settings WHERE id=1").Scan(&clientID); err != nil || clientID != "original.apps.googleusercontent.com" {
		t.Fatal("rejected change altered active client")
	}
	if got := patchGoogleSettings(t, h, original, key); got.Code != 200 {
		t.Fatal("same client should still allow its secret to be updated")
	}
	// A workspace admin can recover connections belonging to an archived member.
	if _, err := database.Exec(`INSERT INTO users(id,name,email,iana_timezone,archived_at) VALUES('departed','Departed','departed@example.com','UTC',datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE calendar_connections SET user_id='departed',is_destination=1 WHERE provider='google'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO connection_calendars(id,user_id,provider,calendar_id) VALUES('stale-choice','departed','google','obsolete-calendar')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO calendar_connections(id,user_id,provider,access_token_enc,calendar_id,is_destination) VALUES('keep-microsoft','departed','microsoft','fixture','primary',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO connection_calendars(id,user_id,provider,calendar_id) VALUES('keep-choice','departed','microsoft','work')`); err != nil {
		t.Fatal(err)
	}
	reset := httptest.NewRecorder()
	h.RequireAuth(h.ResetGoogleConnections)(reset, authReq("POST", "/v1/settings/google/reset-connections", "", key))
	if reset.Code != 200 {
		t.Fatalf("admin reset: %d %s", reset.Code, reset.Body.String())
	}
	var selections int
	if err := database.QueryRow("SELECT count(*) FROM connection_calendars WHERE provider='google'").Scan(&selections); err != nil || selections != 0 {
		t.Fatal("reset retained Google calendar selections")
	}
	var destination int
	if err := database.QueryRow("SELECT is_destination FROM calendar_connections WHERE id='keep-microsoft'").Scan(&destination); err != nil || destination != 1 {
		t.Fatal("reset did not preserve/promote remaining Microsoft destination")
	}
	if err := database.QueryRow("SELECT count(*) FROM connection_calendars WHERE id='keep-choice'").Scan(&selections); err != nil || selections != 1 {
		t.Fatal("reset touched Microsoft calendar selections")
	}
	if got := patchGoogleSettings(t, h, `{"client_id":"replacement.apps.googleusercontent.com","client_secret":"new-fixture-secret"}`, key); got.Code != 200 {
		t.Fatalf("replacement after reset: %d %s", got.Code, got.Body.String())
	}
}

func TestGetGoogleSettings_unconfigured(t *testing.T) {
	h, _, key := newGoogleHandler(t)
	rec := getGoogleSettings(t, h, key)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d; want 200 — %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["client_id"] != "" {
		t.Errorf("client_id = %q; want empty", resp["client_id"])
	}
	if configured, _ := resp["configured"].(bool); configured {
		t.Error("configured = true; want false when no credentials stored")
	}
	if secretSet, _ := resp["client_secret_set"].(bool); secretSet {
		t.Error("client_secret_set = true; want false when no secret stored")
	}
}

func TestGetGoogleSettings_requiresAuth(t *testing.T) {
	h, _, _ := newGoogleHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/settings/google", nil)
	rec := httptest.NewRecorder()
	h.RequireAuth(h.GetGoogleSettings)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got %d; want 401", rec.Code)
	}
}

func TestGetGoogleSettings_nonAdminForbidden(t *testing.T) {
	h, database, _ := newGoogleHandler(t)

	rawKey := "non-admin-google-test-key"
	hash := sha256HexForTest(rawKey)
	database.Exec(`INSERT INTO users (id, email, name, iana_timezone, is_admin) VALUES ('u-ng','ng@example.com','NG','UTC',0)`)
	database.Exec(`INSERT INTO api_keys (id, user_id, name, key_hash, created_at) VALUES ('k-ng','u-ng','test',?,datetime('now'))`, hash)

	req := authReq(http.MethodGet, "/v1/settings/google", "", rawKey)
	rec := httptest.NewRecorder()
	h.RequireAuth(h.GetGoogleSettings)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-admin GET: got %d; want 403", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// PATCH /v1/settings/google — happy paths
// ---------------------------------------------------------------------------

func TestPatchGoogleSettings_savesCredentials(t *testing.T) {
	h, database, key := newGoogleHandler(t)

	rec := patchGoogleSettings(t, h,
		`{"client_id":"test-client-id.apps.googleusercontent.com","client_secret":"s3cr3t"}`, key)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: got %d — %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["client_id"] != "test-client-id.apps.googleusercontent.com" {
		t.Errorf("client_id = %q; want test-client-id.apps.googleusercontent.com", resp["client_id"])
	}
	if configured, _ := resp["configured"].(bool); !configured {
		t.Error("configured = false after saving credentials; want true")
	}
	if secretSet, _ := resp["client_secret_set"].(bool); !secretSet {
		t.Error("client_secret_set = false after saving secret; want true")
	}

	// Verify the secret is encrypted in DB (not plaintext).
	var enc string
	database.QueryRow(`SELECT google_client_secret_enc FROM server_settings WHERE id = 1`).Scan(&enc)
	if enc == "" {
		t.Error("google_client_secret_enc is empty in DB after save")
	}
	if enc == "s3cr3t" {
		t.Error("secret stored as plaintext; want AES-GCM ciphertext")
	}
}

func TestPatchGoogleSettings_keepExistingSecret(t *testing.T) {
	h, database, key := newGoogleHandler(t)

	// First save — set credentials.
	patchGoogleSettings(t, h,
		`{"client_id":"id.apps.googleusercontent.com","client_secret":"original-secret"}`, key)

	var firstEnc string
	database.QueryRow(`SELECT google_client_secret_enc FROM server_settings WHERE id = 1`).Scan(&firstEnc)
	if firstEnc == "" {
		t.Fatal("no secret enc after first patch")
	}

	// Second save — change only client_id, omit secret.
	rec := patchGoogleSettings(t, h,
		`{"client_id":"id.apps.googleusercontent.com"}`, key)
	if rec.Code != http.StatusOK {
		t.Fatalf("second patch: got %d — %s", rec.Code, rec.Body.String())
	}

	var secondEnc string
	database.QueryRow(`SELECT google_client_secret_enc FROM server_settings WHERE id = 1`).Scan(&secondEnc)
	if secondEnc != firstEnc {
		t.Errorf("secret changed when client_secret was omitted; want unchanged")
	}

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if secretSet, _ := resp["client_secret_set"].(bool); !secretSet {
		t.Error("client_secret_set = false after keep-secret patch; want true")
	}
}

func TestPatchGoogleSettings_updateSecret(t *testing.T) {
	h, database, key := newGoogleHandler(t)

	patchGoogleSettings(t, h,
		`{"client_id":"id.apps.googleusercontent.com","client_secret":"old-secret"}`, key)

	var firstEnc string
	database.QueryRow(`SELECT google_client_secret_enc FROM server_settings WHERE id = 1`).Scan(&firstEnc)

	// Send new secret.
	rec := patchGoogleSettings(t, h,
		`{"client_id":"id.apps.googleusercontent.com","client_secret":"new-secret"}`, key)
	if rec.Code != http.StatusOK {
		t.Fatalf("update secret: got %d — %s", rec.Code, rec.Body.String())
	}

	var secondEnc string
	database.QueryRow(`SELECT google_client_secret_enc FROM server_settings WHERE id = 1`).Scan(&secondEnc)
	if secondEnc == firstEnc {
		t.Error("secret ciphertext unchanged after sending new secret; want new ciphertext")
	}
}

func TestPatchGoogleSettings_clearCredentials(t *testing.T) {
	h, database, key := newGoogleHandler(t)

	// Configure first.
	patchGoogleSettings(t, h,
		`{"client_id":"id.apps.googleusercontent.com","client_secret":"s3cr3t"}`, key)

	// Clear by sending empty client_id.
	rec := patchGoogleSettings(t, h, `{"client_id":""}`, key)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: got %d — %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if configured, _ := resp["configured"].(bool); configured {
		t.Error("configured = true after clearing client_id; want false")
	}

	var storedID string
	database.QueryRow(`SELECT google_client_id FROM server_settings WHERE id = 1`).Scan(&storedID)
	if storedID != "" {
		t.Errorf("client_id = %q in DB after clear; want empty", storedID)
	}
}

func TestPatchGoogleSettings_secretNotInResponse(t *testing.T) {
	h, _, key := newGoogleHandler(t)

	rec := patchGoogleSettings(t, h,
		`{"client_id":"id.apps.googleusercontent.com","client_secret":"do-not-leak"}`, key)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: got %d — %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if contains(body, "do-not-leak") {
		t.Error("plaintext client secret leaked in API response")
	}
}

// ---------------------------------------------------------------------------
// PATCH /v1/settings/google — auth & access control
// ---------------------------------------------------------------------------

func TestPatchGoogleSettings_requiresAuth(t *testing.T) {
	h, _, _ := newGoogleHandler(t)
	req := httptest.NewRequest(http.MethodPatch, "/v1/settings/google",
		strReader(`{"client_id":"x","client_secret":"y"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.RequireAuth(h.PatchGoogleSettings)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got %d; want 401", rec.Code)
	}
}

func TestPatchGoogleSettings_nonAdminForbidden(t *testing.T) {
	h, database, _ := newGoogleHandler(t)

	rawKey := "non-admin-patch-google-key"
	hash := sha256HexForTest(rawKey)
	database.Exec(`INSERT INTO users (id, email, name, iana_timezone, is_admin) VALUES ('u-npg','npg@example.com','NPG','UTC',0)`)
	database.Exec(`INSERT INTO api_keys (id, user_id, name, key_hash, created_at) VALUES ('k-npg','u-npg','test',?,datetime('now'))`, hash)

	rec := patchGoogleSettings(t, h, `{"client_id":"evil","client_secret":"evil"}`, rawKey)
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-admin PATCH: got %d; want 403", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// PATCH /v1/settings/google — invalid input
// ---------------------------------------------------------------------------

func TestPatchGoogleSettings_invalidJSON(t *testing.T) {
	h, _, key := newGoogleHandler(t)
	req := authReq(http.MethodPatch, "/v1/settings/google", `{not-json}`, key)
	rec := httptest.NewRecorder()
	h.RequireAuth(h.PatchGoogleSettings)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("got %d; want 400 for invalid JSON", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// CalendarStatus — configured field
// ---------------------------------------------------------------------------

func TestCalendarStatus_configuredField_false_whenGCalNil(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/calendar/status", nil)
	rec := httptest.NewRecorder()
	h.CalendarStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d; want 200", rec.Code)
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if configured, _ := resp["configured"].(bool); configured {
		t.Error("configured = true; want false when gcal is nil")
	}
	if connected, _ := resp["connected"].(bool); connected {
		t.Error("connected = true; want false when gcal is nil")
	}
}

func TestCalendarStatus_configuredField_true_whenGCalSet(t *testing.T) {
	h, _, apiKey, _ := newHandlerWithGCal(t)

	req := authReq(http.MethodGet, "/v1/calendar/status", "", apiKey)
	rec := httptest.NewRecorder()
	h.RequireAuth(h.CalendarStatus)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d; want 200", rec.Code)
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if configured, _ := resp["configured"].(bool); !configured {
		t.Error("configured = false; want true when gcal is set")
	}
}

// ---------------------------------------------------------------------------
// LoadGoogleSettingsFromDB
// ---------------------------------------------------------------------------

func TestLoadGoogleSettingsFromDB_emptyDB_returnsNil(t *testing.T) {
	h, database, key := newGoogleHandler(t)
	_ = h
	_ = key

	var encKey [32]byte
	// decode testGCalKeyHex into encKey
	for i := 0; i < 32; i++ {
		b := testGCalKeyHex[i*2 : i*2+2]
		var v byte
		for _, c := range b {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= byte(c - '0')
			case c >= 'a' && c <= 'f':
				v |= byte(c-'a') + 10
			}
		}
		encKey[i] = v
	}

	cfg, err := handler.LoadGoogleSettingsFromDB(database, encKey)
	if err != nil {
		t.Fatalf("LoadGoogleSettingsFromDB: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil config when client_id is empty; got %+v", cfg)
	}
}

func TestLoadGoogleSettingsFromDB_afterSave_returnsCredentials(t *testing.T) {
	h, database, key := newGoogleHandler(t)

	patchGoogleSettings(t, h,
		`{"client_id":"db-client-id.apps.googleusercontent.com","client_secret":"db-secret"}`, key)

	var encKey [32]byte
	for i := 0; i < 32; i++ {
		b := testGCalKeyHex[i*2 : i*2+2]
		var v byte
		for _, c := range b {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= byte(c - '0')
			case c >= 'a' && c <= 'f':
				v |= byte(c-'a') + 10
			}
		}
		encKey[i] = v
	}

	cfg, err := handler.LoadGoogleSettingsFromDB(database, encKey)
	if err != nil {
		t.Fatalf("LoadGoogleSettingsFromDB: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadGoogleSettingsFromDB returned nil; want credentials")
	}
	if cfg.ClientID != "db-client-id.apps.googleusercontent.com" {
		t.Errorf("ClientID = %q; want db-client-id", cfg.ClientID)
	}
	if cfg.ClientSecret != "db-secret" {
		t.Errorf("ClientSecret = %q; want db-secret (decrypted)", cfg.ClientSecret)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func strReader(s string) *strings.Reader {
	return strings.NewReader(s)
}

func TestGoogleSettingsPreserveOtherCalendarProviders(t *testing.T) {
	h, database, key := newGoogleHandler(t)
	existing := calendar.NewService(database)
	dav, err := caldav.New(database, testGCalKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	existing.Register(dav)
	ms, err := microsoft.New(database, "test-ms", "test-secret", "common", "http://localhost:3000/v1/calendar/callback", testGCalKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	existing.Register(ms)
	h.SetCalendar(existing)
	for _, body := range []string{`{"client_id":"new-google-client.apps.googleusercontent.com","client_secret":"test-secret"}`, `{"client_id":""}`} {
		rec := httptest.NewRecorder()
		h.RequireAuth(h.PatchGoogleSettings)(rec, authReq(http.MethodPatch, "/v1/settings/google", body, key))
		if rec.Code != 200 {
			t.Fatalf("save Google settings: %d %s", rec.Code, rec.Body)
		}
		status := httptest.NewRecorder()
		h.RequireAuth(h.CalendarStatus)(status, authReq(http.MethodGet, "/v1/calendar/status", "", key))
		if status.Code != 200 || !strings.Contains(status.Body.String(), `"caldav"`) || !strings.Contains(status.Body.String(), `"microsoft"`) {
			t.Fatalf("Google settings removed another provider: %d %s", status.Code, status.Body)
		}
	}
	var userID string
	if err := database.QueryRow("SELECT id FROM users LIMIT 1").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	state, err := ms.EncryptState("microsoft\x1f" + userID)
	if err != nil {
		t.Fatal(err)
	}
	callback := httptest.NewRecorder()
	h.CalendarCallback(callback, httptest.NewRequest(http.MethodGet, "/v1/calendar/callback?state="+url.QueryEscape(state), nil))
	if callback.Code != 400 || !strings.Contains(callback.Body.String(), "missing code") {
		t.Fatalf("Microsoft OAuth state stopped routing after Google removal: %d %s", callback.Code, callback.Body)
	}
}

func TestGoogleSettingsPreserveEnvironmentSecretOnSave(t *testing.T) {
	h, database, key := newGoogleHandler(t)
	h.SetGoogleAuth("environment-client.apps.googleusercontent.com", "environment-secret", "http://localhost:3000/v1/auth/callback", false)
	rec := getGoogleSettings(t, h, key)
	if !strings.Contains(rec.Body.String(), `"client_id":"environment-client.apps.googleusercontent.com"`) || !strings.Contains(rec.Body.String(), `"client_secret_set":true`) {
		t.Fatal("active environment configuration is hidden")
	}
	saved := httptest.NewRecorder()
	h.RequireAuth(h.PatchGoogleSettings)(saved, authReq(http.MethodPatch, "/v1/settings/google", `{"client_id":"environment-client.apps.googleusercontent.com"}`, key))
	if saved.Code != 200 {
		t.Fatalf("save: %d", saved.Code)
	}
	var encoded string
	if err := database.QueryRow("SELECT google_client_secret_enc FROM server_settings WHERE id=1").Scan(&encoded); err != nil || encoded == "" {
		t.Fatal("saving lost the environment-sourced secret")
	}
	if strings.Contains(saved.Body.String(), "environment-secret") {
		t.Fatal("secret exposed in response")
	}
}

func TestGoogleSettingsDropsGmailWhenPreviousOAuthIsMissing(t *testing.T) {
	h, database, key := newGoogleHandler(t)
	var userID string
	database.QueryRow("SELECT id FROM users LIMIT 1").Scan(&userID)
	if _, err := database.Exec("INSERT INTO app_integrations(provider,generation,credentials_enc,config,connected_by,connected_at) VALUES('gmail','old','old-cipher','{}',?,'2026-10-12T00:00:00Z')", userID); err != nil {
		t.Fatal(err)
	}
	h.InitAppConnectors(mailer.NewLive(&mailer.Noop{}))
	rec := httptest.NewRecorder()
	h.RequireAuth(h.PatchGoogleSettings)(rec, authReq(http.MethodPatch, "/v1/settings/google", `{"client_id":"new-client.apps.googleusercontent.com","client_secret":"new-secret"}`, key))
	if rec.Code != 200 {
		t.Fatalf("save: %d", rec.Code)
	}
	var remaining int
	database.QueryRow("SELECT COUNT(*) FROM app_integrations WHERE provider='gmail'").Scan(&remaining)
	if remaining != 0 {
		t.Fatal("stale Gmail credentials survived an unknown previous OAuth configuration")
	}
}

func TestGoogleSettingsRejectInvalidPairBeforeChangingConnections(t *testing.T) {
	h, database, key := newGoogleHandler(t)
	valid := `{"client_id":"existing.apps.googleusercontent.com","client_secret":"existing-secret"}`
	if rec := patchGoogleSettings(t, h, valid, key); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	for _, body := range []string{
		`{"client_id":"not-an-oauth-client","client_secret":"some-secret"}`,
		`{"client_id":"different.apps.googleusercontent.com"}`,
	} {
		rec := patchGoogleSettings(t, h, body, key)
		if rec.Code != 400 {
			t.Errorf("invalid pair accepted: %d", rec.Code)
		}
		var id string
		database.QueryRow("SELECT google_client_id FROM server_settings WHERE id=1").Scan(&id)
		if id != "existing.apps.googleusercontent.com" {
			t.Error("invalid save changed stored credentials")
		}
	}
}

func TestGoogleSettingsIncompletePairIsNotConfigured(t *testing.T) {
	for _, tc := range []struct{ id, secret string }{
		{"project-id", "encrypted-placeholder"},
		{"valid.apps.googleusercontent.com", ""},
		{"unreadable.apps.googleusercontent.com", "not-decryptable"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			h, database, key := newGoogleHandler(t)
			if _, err := database.Exec("UPDATE server_settings SET google_client_id=?,google_client_secret_enc=? WHERE id=1", tc.id, tc.secret); err != nil {
				t.Fatal(err)
			}
			rec := getGoogleSettings(t, h, key)
			var response struct {
				Configured         bool   `json:"configured"`
				ConfigurationError string `json:"configuration_error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Configured || response.ConfigurationError == "" {
				t.Fatalf("invalid pair presented as ready: %s", rec.Body.String())
			}
		})
	}
}
