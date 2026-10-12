package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

// Google flows bind an S256 verifier to a single-use database state and the
// initiating browser. A copied callback URL cannot connect another browser.
func (h *Handler) beginGoogleOAuth(w http.ResponseWriter, r *http.Request, purpose, userID string) (string, string, error) {
	state := "google." + oauth2.GenerateVerifier()
	verifier := oauth2.GenerateVerifier()
	now := time.Now().UTC()
	_, _ = h.db.ExecContext(r.Context(), "DELETE FROM google_oauth_states WHERE expires_at < ?", now.Format(time.RFC3339Nano))
	_, err := h.db.ExecContext(r.Context(), "INSERT INTO google_oauth_states(state_hash,purpose,user_id,verifier,expires_at) VALUES(?,?,?,?,?)", stateHash(state), purpose, sql.NullString{String: userID, Valid: userID != ""}, verifier, now.Add(stateDuration).Format(time.RFC3339Nano))
	if err != nil {
		return "", "", err
	}
	h.googleStateCookie(w, purpose, state, int(stateDuration.Seconds()))
	return state, verifier, nil
}

func (h *Handler) googleStateCookie(w http.ResponseWriter, purpose, value string, maxAge int) {
	path := "/v1/auth/callback"
	name := stateCookieName
	if purpose == "calendar" {
		path = "/v1/calendar/callback"
		name = "openslot_google_calendar_state"
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: path, MaxAge: maxAge, HttpOnly: true, Secure: h.secureCookie, SameSite: http.SameSiteLaxMode})
}

func (h *Handler) consumeGoogleOAuth(w http.ResponseWriter, r *http.Request, purpose string) (string, string, error) {
	name := stateCookieName
	if purpose == "calendar" {
		name = "openslot_google_calendar_state"
	}
	cookie, err := r.Cookie(name)
	state := r.URL.Query().Get("state")
	h.googleStateCookie(w, purpose, "", -1)
	if err != nil || state == "" || cookie.Value != state {
		return "", "", errors.New("Google sign-in state is invalid; connect again")
	}
	var userID sql.NullString
	var verifier string
	err = h.db.QueryRowContext(r.Context(), `DELETE FROM google_oauth_states WHERE state_hash=? AND purpose=? AND expires_at>? RETURNING user_id,verifier`, stateHash(state), purpose, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&userID, &verifier)
	if err != nil {
		return "", "", errors.New("Google sign-in expired or was already used; connect again")
	}
	return userID.String, verifier, nil
}
