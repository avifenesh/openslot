package handler

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/calnode/calnode/internal/calendar"
	"github.com/calnode/calnode/internal/gcal"
	"github.com/calnode/calnode/internal/googleoauth"
	"github.com/calnode/calnode/internal/secret"
)

// GoogleOAuthConfig holds decrypted Google OAuth settings loaded from the DB.
// Used by server.go to build the initial gcal and auth clients on startup.
type GoogleOAuthConfig struct {
	ClientID     string
	ClientSecret string
	ClientType   string
}

// LoadGoogleSettingsFromDB reads Google OAuth credentials from server_settings
// and decrypts the client secret. Returns nil (not an error) when client_id is empty.
func LoadGoogleSettingsFromDB(db *sql.DB, encKey [32]byte) (*GoogleOAuthConfig, error) {
	var clientID, secretEnc, clientType string
	err := db.QueryRow(`
		SELECT google_client_id, google_client_secret_enc, google_client_type
		FROM server_settings WHERE id = 1`).
		Scan(&clientID, &secretEnc, &clientType)
	if err == sql.ErrNoRows || clientID == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var clientSecret string
	if secretEnc != "" {
		clientSecret, err = secret.Decrypt(encKey, secretEnc)
		if err != nil {
			return nil, fmt.Errorf("decrypt google client secret: %w", err)
		}
	}
	return &GoogleOAuthConfig{ClientID: clientID, ClientSecret: clientSecret, ClientType: clientType}, nil
}

// GetGoogleSettings handles GET /v1/settings/google (admin only).
func (h *Handler) GetGoogleSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	var clientID, secretEnc, clientType string
	err := h.db.QueryRowContext(r.Context(), `
		SELECT google_client_id, google_client_secret_enc, google_client_type
		FROM server_settings WHERE id = 1`).
		Scan(&clientID, &secretEnc, &clientType)
	if err != nil && err != sql.ErrNoRows {
		h.logger.ErrorContext(r.Context(), "google settings: query", "error", err)
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	secretSet := secretEnc != ""
	if clientID == "" {
		if active := h.getGoogleAuth(); active != nil {
			clientID = active.ClientID
			secretSet = active.ClientSecret != ""
			secretEnc = "" // The active environment pair is authoritative when no DB client is selected.
		}
	}
	problem := googleoauth.Problem(clientID, secretSet)
	if clientType == "desktop" && !h.googleDesktopAllowed {
		problem = "This Google Desktop app requires OpenSlot to listen on localhost. Import a Web application client for a hosted server."
	}
	if secretEnc != "" {
		decrypted, err := secret.Decrypt(h.encKey, secretEnc)
		if err != nil {
			problem = "The stored Google Client Secret cannot be read. Enter the matching Client Secret again."
		} else if decrypted == "" {
			problem = googleoauth.Problem(clientID, false)
		}
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"client_id":           clientID,
		"client_type":         clientType,
		"desktop_allowed":     h.googleDesktopAllowed,
		"client_secret_set":   secretSet,
		"configured":          problem == "",
		"configuration_error": problem,
		// base_url is the identity host the server builds OAuth redirect URIs
		// from (see PatchGoogleSettings); the setup UI renders the exact URIs to
		// register in Google Cloud so they always match what we send.
		"base_url": h.baseURL,
	})
}

// PatchGoogleSettings handles PATCH /v1/settings/google (admin only).
// Saves credentials to the DB and hot-reloads the gcal and Google auth clients
// so changes take effect immediately without a server restart.
// If client_secret is omitted or empty the existing stored secret is kept.
func (h *Handler) PatchGoogleSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	if h.demoMode {
		h.writeError(w, http.StatusServiceUnavailable, "not available in the demo")
		return
	}
	h.googleSetupMu.Lock()
	defer h.googleSetupMu.Unlock()
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var req struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		ClientType   string `json:"client_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	req.ClientID = strings.TrimSpace(req.ClientID)
	req.ClientSecret = strings.TrimSpace(req.ClientSecret)
	previousGoogle := h.getGoogleAuth()
	if req.ClientType == "" {
		h.authMu.RLock()
		req.ClientType = h.googleClientType
		h.authMu.RUnlock()
		if req.ClientType == "" {
			req.ClientType = "web"
		}
	}
	if req.ClientType != "web" && req.ClientType != "desktop" {
		h.writeError(w, 400, "Google client type must be web or desktop")
		return
	}
	if req.ClientID != "" && req.ClientType == "desktop" && !h.googleDesktopAllowed {
		h.writeError(w, 400, "A Desktop app can only connect to a local OpenSlot installation. Import a Web application client for this server.")
		return
	}
	if req.ClientID != "" {
		secretAvailable := req.ClientSecret != "" || (previousGoogle != nil && previousGoogle.ClientID == req.ClientID && previousGoogle.ClientSecret != "")
		if problem := googleoauth.Problem(req.ClientID, secretAvailable); problem != "" {
			h.writeError(w, http.StatusBadRequest, problem)
			return
		}
	}
	// Existing refresh tokens belong to their original OAuth client. Never leave
	// working calendars attached to a replacement client that cannot refresh them.
	var storedClientID string
	if err := h.db.QueryRowContext(r.Context(), "SELECT google_client_id FROM server_settings WHERE id=1").Scan(&storedClientID); err != nil && err != sql.ErrNoRows {
		h.writeError(w, 500, "could not read Google setup")
		return
	}
	if storedClientID == "" && previousGoogle != nil {
		storedClientID = previousGoogle.ClientID
	}
	if req.ClientID != storedClientID {
		var connected int
		if err := h.db.QueryRowContext(r.Context(), "SELECT count(*) FROM calendar_connections WHERE provider='google'").Scan(&connected); err != nil {
			h.writeError(w, 500, "could not check Google connections")
			return
		}
		if connected > 0 {
			h.writeError(w, http.StatusConflict, "Use Reset Google connections in Google setup before replacing or removing this app. This also handles calendars belonging to archived members.")
			return
		}
	}
	// Preserve a usable stored ciphertext on an unchanged client. If startup
	// fell back to matching environment credentials, persist that active secret.
	if previousGoogle != nil && req.ClientID == previousGoogle.ClientID && req.ClientSecret == "" {
		var stored string
		err := h.db.QueryRowContext(r.Context(), "SELECT google_client_secret_enc FROM server_settings WHERE id=1").Scan(&stored)
		if err != nil && err != sql.ErrNoRows {
			h.writeError(w, http.StatusInternalServerError, "could not read saved Google settings")
			return
		}
		decoded, decryptErr := secret.Decrypt(h.encKey, stored)
		if stored == "" || decryptErr != nil || decoded == "" {
			req.ClientSecret = previousGoogle.ClientSecret
		}
	}
	if h.apps != nil && (req.ClientID == "" || previousGoogle == nil || previousGoogle.ClientID != req.ClientID) {
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		if err := h.apps.Disconnect(ctx, "gmail"); err != nil {
			h.writeError(w, http.StatusServiceUnavailable, "could not disconnect Gmail before changing Google OAuth; try again")
			return
		}
	}
	if req.ClientID == "" {
		// Clearing credentials — wipe both columns so the encrypted secret
		// doesn't linger in the DB after the admin removes Google OAuth.
		if _, err := h.db.ExecContext(r.Context(), `
			UPDATE server_settings SET
			  google_client_id = '', google_client_secret_enc = '',
		  google_client_type = 'web',
			  updated_at = datetime('now')
			WHERE id = 1`); err != nil {
			h.logger.ErrorContext(r.Context(), "google settings: clear", "error", err)
			h.writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		h.replaceGoogleCalendar(nil)
		h.authMu.Lock()
		h.googleAuth = nil
		h.authMu.Unlock()
		h.GetGoogleSettings(w, r)
		return
	}

	if req.ClientSecret != "" {
		enc, err := secret.Encrypt(h.encKey, req.ClientSecret)
		if err != nil {
			h.logger.ErrorContext(r.Context(), "google settings: encrypt secret", "error", err)
			h.writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if _, err = h.db.ExecContext(r.Context(), `
			UPDATE server_settings SET
			  google_client_id = ?, google_client_secret_enc = ?, google_client_type = ?,
			  updated_at = datetime('now')
			WHERE id = 1`, req.ClientID, enc, req.ClientType); err != nil {
			h.logger.ErrorContext(r.Context(), "google settings: update", "error", err)
			h.writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	} else {
		if _, err := h.db.ExecContext(r.Context(), `
			UPDATE server_settings SET
			  google_client_id = ?, google_client_type = ?,
			  updated_at = datetime('now')
			WHERE id = 1`, req.ClientID, req.ClientType); err != nil {
			h.logger.ErrorContext(r.Context(), "google settings: update (keep secret)", "error", err)
			h.writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	// Hot-reload: resolve the active secret and reinitialise gcal + Google auth
	// so changes take effect without a server restart.
	resolvedSecret := req.ClientSecret
	if resolvedSecret == "" {
		var secretEnc string
		if err := h.db.QueryRowContext(r.Context(),
			`SELECT google_client_secret_enc FROM server_settings WHERE id = 1`).
			Scan(&secretEnc); err != nil {
			h.logger.ErrorContext(r.Context(), "google settings: re-read secret", "error", err)
			h.writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if secretEnc != "" {
			var decErr error
			resolvedSecret, decErr = secret.Decrypt(h.encKey, secretEnc)
			if decErr != nil {
				h.logger.ErrorContext(r.Context(), "google settings: decrypt existing secret", "error", decErr)
				h.writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		}
	}

	if resolvedSecret != "" {
		encKeyHex := hex.EncodeToString(h.encKey[:])
		gc, err := gcal.New(h.db, req.ClientID, resolvedSecret, h.baseURL+"/v1/calendar/callback", encKeyHex)
		if err != nil {
			h.logger.ErrorContext(r.Context(), "google settings: hot-reload gcal failed", "error", err)
			h.writeError(w, http.StatusInternalServerError, "failed to initialize calendar client")
			return
		}
		h.replaceGoogleCalendar(gc)
		h.SetGoogleAuthType(req.ClientID, resolvedSecret, h.baseURL+"/v1/auth/callback", h.secureCookie, req.ClientType)
		h.logger.Info("google settings: credentials updated and gcal hot-reloaded")
	}

	h.GetGoogleSettings(w, r)
}

// ResetGoogleConnections lets an administrator recover a workspace even when
// a departed member cannot sign in to disconnect their calendar themselves.
func (h *Handler) ResetGoogleConnections(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	if h.demoMode {
		h.writeError(w, 503, "not available in the demo")
		return
	}
	h.googleSetupMu.Lock()
	defer h.googleSetupMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if h.apps != nil {
		if err := h.apps.Disconnect(ctx, "gmail"); err != nil {
			h.writeError(w, 503, "Could not disconnect Gmail. Try again.")
			return
		}
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		h.writeError(w, 500, "could not reset Google connections")
		return
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT DISTINCT user_id FROM calendar_connections WHERE provider='google' AND is_destination=1")
	if err != nil {
		h.writeError(w, 500, "could not reset Google connections")
		return
	}
	var users []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		users = append(users, id)
	}
	rows.Close()
	if err != nil || rows.Err() != nil {
		h.writeError(w, 500, "could not reset Google connections")
		return
	}
	for _, statement := range []string{"DELETE FROM calendar_connections WHERE provider='google'", "DELETE FROM connection_calendars WHERE provider='google'", "DELETE FROM google_oauth_states WHERE purpose='calendar'", "DELETE FROM app_oauth_states"} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			h.writeError(w, 500, "could not reset Google connections")
			return
		}
	}
	for _, id := range users {
		if _, err = tx.ExecContext(ctx, `UPDATE calendar_connections SET is_destination=1 WHERE id=(SELECT id FROM calendar_connections WHERE user_id=? ORDER BY created_at,id LIMIT 1) AND NOT EXISTS(SELECT 1 FROM calendar_connections WHERE user_id=? AND is_destination=1)`, id, id); err != nil {
			h.writeError(w, 500, "could not reset Google connections")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		h.writeError(w, 500, "could not reset Google connections")
		return
	}
	h.writeJSON(w, 200, map[string]any{"ok": true})
}

// Build a replacement service rather than mutating providers used by active requests.
// Google goes first because the OAuth callback decrypts state through Primary().
func (h *Handler) replaceGoogleCalendar(google calendar.Provider) {
	next := calendar.NewService(h.db)
	if google != nil {
		next.Register(google)
	}
	if previous := h.getCal(); previous != nil {
		if google == nil {
			if microsoft := previous.Provider("microsoft"); microsoft != nil {
				next.Register(microsoft)
			}
		}
		if primary := previous.Primary(); primary != nil && primary.Name() != "google" {
			next.Register(primary)
		}
		for _, name := range previous.ProviderNames() {
			if name != "google" {
				next.Register(previous.Provider(name))
			}
		}
	}
	if next.Any() {
		h.SetCalendar(next)
	} else {
		h.SetCalendar(nil)
	}
}
