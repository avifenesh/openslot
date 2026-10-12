package handler_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

type googleTransport func(*http.Request) (*http.Response, error)

func (f googleTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGoogleCalendarPKCECallback(t *testing.T) {
	h, _, key, _ := newHandlerWithGCal(t)
	start := httptest.NewRecorder()
	h.RequireAuth(h.ConnectCalendar)(start, authReq("GET", "/v1/calendar/connect?provider=google", "", key))
	location, _ := url.Parse(start.Header().Get("Location"))
	challenge := location.Query().Get("code_challenge")
	if challenge == "" || location.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("Google Calendar must start with PKCE S256")
	}
	exchanges := 0
	client := &http.Client{Transport: googleTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"id":"owner@example.com"}`
		if r.Method == "POST" {
			exchanges++
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			verifier := r.Form.Get("code_verifier")
			sum := sha256.Sum256([]byte(verifier))
			if verifier == "" || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
				t.Fatal("token exchange missing matching PKCE verifier")
			}
			body = `{"access_token":"fixture-access","refresh_token":"fixture-refresh","token_type":"Bearer","expires_in":3600}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	callback := func(withCookie bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/v1/calendar/callback?state="+url.QueryEscape(location.Query().Get("state"))+"&code=fixture-code", nil)
		if withCookie {
			for _, cookie := range start.Result().Cookies() {
				req.AddCookie(cookie)
			}
		}
		req = req.WithContext(context.WithValue(req.Context(), oauth2.HTTPClient, client))
		rec := httptest.NewRecorder()
		h.CalendarCallback(rec, req)
		return rec
	}
	if got := callback(false); got.Code != 400 {
		t.Fatalf("missing browser cookie: %d", got.Code)
	}
	if exchanges != 0 {
		t.Fatal("exchanged before validating browser state")
	}
	if got := callback(true); got.Code != 302 || !strings.Contains(got.Header().Get("Location"), "connected=true") {
		t.Fatalf("callback: %d %s", got.Code, got.Body.String())
	}
	if got := callback(true); got.Code != 400 {
		t.Fatalf("replayed callback: %d", got.Code)
	}
	if exchanges != 1 {
		t.Fatalf("token exchanges: %d", exchanges)
	}
}

func TestGoogleCalendarExpiredState(t *testing.T) {
	h, database, key := newGoogleHandler(t)
	if got := patchGoogleSettings(t, h, `{"client_id":"fixture.apps.googleusercontent.com","client_secret":"fixture-secret"}`, key); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	start := httptest.NewRecorder()
	h.RequireAuth(h.ConnectCalendar)(start, authReq("GET", "/v1/calendar/connect?provider=google", "", key))
	location, _ := url.Parse(start.Header().Get("Location"))
	if _, err := database.Exec("UPDATE google_oauth_states SET expires_at='2000-01-01T00:00:00Z'"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/v1/calendar/callback?state="+url.QueryEscape(location.Query().Get("state"))+"&code=unused", nil)
	for _, cookie := range start.Result().Cookies() {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.CalendarCallback(rec, req)
	if rec.Code != 400 {
		t.Fatalf("expired state: %d", rec.Code)
	}
}
