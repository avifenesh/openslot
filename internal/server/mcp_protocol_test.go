package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calnode/calnode/internal/config"
	"github.com/calnode/calnode/internal/db"
	"github.com/calnode/calnode/internal/handler"
)

func TestRemoteMCPCurrentProtocolAndAuthentication(t *testing.T) {
	database, err := db.Open("sqlite://:memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err = db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	seed := handler.New(database, logger)
	rec := httptest.NewRecorder()
	seed.Setup(rec, httptest.NewRequest(http.MethodPost, "/v1/setup", strings.NewReader(`{"name":"Host","email":"host@example.test","timezone":"UTC"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body)
	}
	var identity struct {
		APIKey string `json:"api_key"`
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &identity); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	app, drain := New(ctx, &config.Config{BaseURL: "http://localhost:3000", DataDir: t.TempDir()}, database, logger)
	t.Cleanup(func() { cancel(); drain() })
	call := func(key, protocol string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":%q,"io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"openslot-test","version":"1.0.0"}}}}`, protocol)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("MCP-Protocol-Version", protocol)
		r.Header.Set("Mcp-Method", "tools/list")
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		return w
	}
	if got := call("", "2026-07-28"); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d", got.Code)
	}
	if got := call("invalid", "2026-07-28"); got.Code != http.StatusUnauthorized {
		t.Fatalf("invalid key: %d", got.Code)
	}
	got := call(identity.APIKey, "2026-07-28")
	if got.Code != http.StatusOK {
		t.Fatalf("current protocol without handshake: %d %s", got.Code, got.Body)
	}
	for _, name := range []string{"get_available_slots", "create_booking", "reschedule_booking", "cancel_booking"} {
		if !strings.Contains(got.Body.String(), name) {
			t.Errorf("missing booking tool %s", name)
		}
	}
	// Existing MCP clients still speak the previous revision.
	if legacy := call(identity.APIKey, "2025-11-25"); legacy.Code != http.StatusOK || !strings.Contains(legacy.Body.String(), "create_booking") {
		t.Fatalf("legacy protocol: %d %s", legacy.Code, legacy.Body)
	}
	if _, err = database.Exec("DELETE FROM api_keys"); err != nil {
		t.Fatal(err)
	}
	if got = call(identity.APIKey, "2026-07-28"); got.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key: %d", got.Code)
	}
}
