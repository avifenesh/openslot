package handler

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/calnode/calnode/internal/calendar"
	"github.com/calnode/calnode/internal/gcal"
	"github.com/calnode/calnode/internal/secret"
)

// GoogleOAuthConfig holds decrypted Google OAuth settings loaded from the DB.
// Used by server.go to build the initial gcal and auth clients on startup.
type GoogleOAuthConfig struct {
	ClientID     string
	ClientSecret string
}

// LoadGoogleSettingsFromDB reads Google OAuth credentials from server_settings
// and decrypts the client secret. Returns nil (not an error) when client_id is empty.
func LoadGoogleSettingsFromDB(db *sql.DB, encKey [32]byte) (*GoogleOAuthConfig, error) {
	var clientID, secretEnc string
	err := db.QueryRow(`
		SELECT google_client_id, google_client_secret_enc
		FROM server_settings WHERE id = 1`).
		Scan(&clientID, &secretEnc)
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
	return &GoogleOAuthConfig{ClientID: clientID, ClientSecret: clientSecret}, nil
}

// GetGoogleSettings handles GET /v1/settings/google (admin only).
func (h *Handler) GetGoogleSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	var clientID, secretEnc string
	err := h.db.QueryRowContext(r.Context(), `
		SELECT google_client_id, google_client_secret_enc
		FROM server_settings WHERE id = 1`).
		Scan(&clientID, &secretEnc)
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
		}
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"client_id":         clientID,
		"client_secret_set": secretSet,
		"configured":        clientID != "",
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
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var req struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	previousGoogle := h.getGoogleAuth()
	// Persist an unchanged environment-sourced secret when the operator saves the form.
	// Otherwise a DB client ID with an empty secret would override the working environment on restart.
	if previousGoogle != nil && req.ClientID == previousGoogle.ClientID && req.ClientSecret == "" {
		var stored string
		if err := h.db.QueryRowContext(r.Context(), "SELECT google_client_secret_enc FROM server_settings WHERE id=1").Scan(&stored); err == nil && stored == "" {
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
			  google_client_id = ?, google_client_secret_enc = ?,
			  updated_at = datetime('now')
			WHERE id = 1`, req.ClientID, enc); err != nil {
			h.logger.ErrorContext(r.Context(), "google settings: update", "error", err)
			h.writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	} else {
		if _, err := h.db.ExecContext(r.Context(), `
			UPDATE server_settings SET
			  google_client_id = ?,
			  updated_at = datetime('now')
			WHERE id = 1`, req.ClientID); err != nil {
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
		h.SetGoogleAuth(req.ClientID, resolvedSecret, h.baseURL+"/v1/auth/callback", h.secureCookie)
		h.logger.Info("google settings: credentials updated and gcal hot-reloaded")
	}

	h.GetGoogleSettings(w, r)
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
