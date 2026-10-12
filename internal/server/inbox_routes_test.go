package server

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/calnode/calnode/internal/config"
	"github.com/calnode/calnode/internal/db"
)

func TestInboxRoutesRequireAuthentication(t *testing.T) {
	database, e := db.Open("sqlite://:memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer database.Close()
	if e = db.Migrate(database); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	app, drain := New(ctx, &config.Config{BaseURL: "http://localhost:8788", DataDir: t.TempDir()}, database, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer func() { cancel(); drain() }()
	for _, route := range []struct{ method, path string }{{"GET", "/v1/inbox"}, {"PUT", "/v1/inbox"}, {"POST", "/v1/inbox/preview"}, {"POST", "/v1/inbox/claim"}, {"GET", "/v1/inbox/requests/test"}, {"POST", "/mcp/inbox"}} {
		t.Run(route.method+route.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			app.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
			if w.Code != 401 {
				t.Fatalf("got %d; inbox route must exist and require authentication", w.Code)
			}
		})
	}
}
