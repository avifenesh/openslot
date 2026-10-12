package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEventTypeListExposesConfiguredPublicOrigin(t *testing.T) {
	h, _, key, _ := setupWorkspaceWithDB(t)
	h.SetBaseURL("https://admin.example.test")
	h.SetPublicBaseURL("https://booking.example.test/")
	rec := httptest.NewRecorder()
	h.RequireAuth(h.ListEventTypes)(rec, authReq(http.MethodGet, "/v1/event-types", "", key))
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	var out struct {
		PublicBaseURL string `json:"public_base_url"`
	}
	if e := json.Unmarshal(rec.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	if out.PublicBaseURL != "https://booking.example.test" {
		t.Fatal("sharing must use public origin", out.PublicBaseURL)
	}
}
