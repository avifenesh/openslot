package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/calnode/calnode/internal/appconnectors"
	"github.com/calnode/calnode/internal/mailer"
	"golang.org/x/oauth2"
)

func (h *Handler) InitAppConnectors(fallback *mailer.Live) *appconnectors.Service {
	h.apps = appconnectors.New(h.db, h.encKey, func() *oauth2.Config {
		existing := h.getGoogleAuth()
		if existing == nil {
			return nil
		}
		config := *existing
		config.RedirectURL = strings.TrimRight(h.baseURL, "/") + "/v1/apps/gmail/callback"
		return &config
	}, fallback)
	return h.apps
}

func (h *Handler) AppConnections(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	if h.apps == nil {
		h.writeError(w, http.StatusServiceUnavailable, "app connectors are not initialized")
		return
	}
	result := []appconnectors.Status{}
	for _, provider := range []string{"gmail", "notion"} {
		status, err := h.apps.Status(r.Context(), provider)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "could not load app connections")
			return
		}
		result = append(result, status)
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"items": result, "gmail_oauth_configured": h.getGoogleAuth() != nil, "gmail_callback_url": strings.TrimRight(h.baseURL, "/") + "/v1/apps/gmail/callback"})
}

func (h *Handler) appAdmin(w http.ResponseWriter, r *http.Request) (AuthUser, bool) {
	user, ok := h.requireAdmin(w, r)
	if !ok {
		return AuthUser{}, false
	}
	if h.demoMode {
		h.writeError(w, http.StatusServiceUnavailable, "connectors are disabled in the demo")
		return AuthUser{}, false
	}
	if h.apps == nil {
		h.writeError(w, http.StatusServiceUnavailable, "app connectors are not initialized")
		return AuthUser{}, false
	}
	return user, true
}

const gmailStateCookie = "openslot_gmail_oauth_state"

func stateHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (h *Handler) ConnectGmail(w http.ResponseWriter, r *http.Request) {
	user, ok := h.appAdmin(w, r)
	if !ok {
		return
	}
	config := h.apps.GoogleConfig()
	if config == nil {
		h.writeError(w, http.StatusBadRequest, "configure Google OAuth in Settings first")
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		h.writeError(w, 500, "could not start Google sign-in")
		return
	}
	state := hex.EncodeToString(raw)
	verifier := oauth2.GenerateVerifier()
	expires := time.Now().Add(stateDuration).UTC().Format(time.RFC3339)
	_, _ = h.db.ExecContext(r.Context(), "DELETE FROM app_oauth_states WHERE expires_at < ?", time.Now().UTC().Format(time.RFC3339))
	if _, err := h.db.ExecContext(r.Context(), "INSERT INTO app_oauth_states(state_hash,user_id,verifier,expires_at) VALUES(?,?,?,?)", stateHash(state), user.ID, verifier, expires); err != nil {
		h.writeError(w, 500, "could not save Google sign-in state")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: gmailStateCookie, Value: state, Path: "/v1/apps/gmail/callback", HttpOnly: true, Secure: h.secureCookie, SameSite: http.SameSiteLaxMode, MaxAge: int(stateDuration.Seconds())})
	location := config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"), oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, location, http.StatusFound)
}

func (h *Handler) GmailCallback(w http.ResponseWriter, r *http.Request) {
	user, ok := h.appAdmin(w, r)
	if !ok {
		return
	}
	h.googleSetupMu.Lock()
	defer h.googleSetupMu.Unlock()
	cookie, err := r.Cookie(gmailStateCookie)
	state := r.URL.Query().Get("state")
	http.SetCookie(w, &http.Cookie{Name: gmailStateCookie, Value: "", Path: "/v1/apps/gmail/callback", HttpOnly: true, Secure: h.secureCookie, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	if err != nil || state == "" || cookie.Value != state {
		h.writeError(w, http.StatusBadRequest, "Google sign-in state is invalid; reconnect Gmail")
		return
	}
	var verifier string
	err = h.db.QueryRowContext(r.Context(), `DELETE FROM app_oauth_states WHERE state_hash=? AND user_id=? AND expires_at>? RETURNING verifier`, stateHash(state), user.ID, time.Now().UTC().Format(time.RFC3339)).Scan(&verifier)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "Google sign-in expired or was already used; reconnect Gmail")
		return
	}
	if r.URL.Query().Get("error") != "" {
		http.Redirect(w, r, "/admin/apps?error=gmail_denied", http.StatusSeeOther)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		h.writeError(w, http.StatusBadRequest, "Google did not return an authorization code")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if err = h.apps.ExchangeGmail(ctx, user.ID, code, verifier); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, "/admin/apps?connected=gmail", http.StatusSeeOther)
}

func (h *Handler) ConnectNotion(w http.ResponseWriter, r *http.Request) {
	user, ok := h.appAdmin(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var req struct {
		Token  string `json:"token"`
		Parent string `json:"parent_page_id"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		h.writeError(w, 400, "invalid JSON")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if err := h.apps.ConnectNotion(ctx, user.ID, strings.TrimSpace(req.Token), req.Parent); err != nil {
		h.writeError(w, 400, err.Error())
		return
	}
	h.AppConnections(w, r)
}

func (h *Handler) CheckAppConnection(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.appAdmin(w, r); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	var err error
	switch r.PathValue("provider") {
	case "gmail":
		err = h.apps.CheckGmail(ctx)
	case "notion":
		err = h.apps.CheckNotion(ctx)
	default:
		h.writeError(w, 404, "unknown app connector")
		return
	}
	if err != nil {
		h.writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.writeJSON(w, 200, map[string]any{"ok": true, "message": "Account access verified. No emails sent and no pages created."})
}

func (h *Handler) DisconnectApp(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.appAdmin(w, r); !ok {
		return
	}
	provider := r.PathValue("provider")
	if provider != "gmail" && provider != "notion" {
		h.writeError(w, 404, "unknown app connector")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if err := h.apps.Disconnect(ctx, provider); err != nil {
		h.writeError(w, 500, "could not disconnect app")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) SyncNotionBookings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.appAdmin(w, r); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	count, err := h.apps.QueueNotionBackfill(ctx)
	if err != nil {
		h.writeError(w, 400, err.Error())
		return
	}
	h.writeJSON(w, http.StatusAccepted, map[string]any{"queued": count})
}
