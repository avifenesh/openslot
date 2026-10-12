// Package appconnectors manages workspace mail and booking export integrations.
package appconnectors

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/calnode/calnode/internal/mailer"
	"github.com/calnode/calnode/internal/secret"
	"github.com/calnode/calnode/internal/uid"
	"golang.org/x/oauth2"
	"golang.org/x/time/rate"
)

type Config struct {
	InboxAccess  bool   `json:"inbox_access"`
	Email        string `json:"email,omitempty"`
	Name         string `json:"name,omitempty"`
	DatabaseID   string `json:"database_id,omitempty"`
	DataSourceID string `json:"data_source_id,omitempty"`
	URL          string `json:"url,omitempty"`
}

type Status struct {
	Provider    string  `json:"provider"`
	Connected   bool    `json:"connected"`
	Config      Config  `json:"config"`
	ConnectedAt string  `json:"connected_at,omitempty"`
	LastSuccess *string `json:"last_success_at"`
	Error       string  `json:"error,omitempty"`
}

type Service struct {
	db            *sql.DB
	key           [32]byte
	client        *http.Client
	google        func() *oauth2.Config
	fallback      *mailer.Live
	gmailGate     chan struct{}
	gmailActive   atomic.Bool
	notionGate    chan struct{}
	notionRate    *rate.Limiter
	notionRetryAt atomic.Int64
}

func New(db *sql.DB, key [32]byte, google func() *oauth2.Config, fallback *mailer.Live) *Service {
	service := &Service{gmailGate: make(chan struct{}, 1), notionGate: make(chan struct{}, 1), db: db, key: key, google: google, fallback: fallback, notionRate: rate.NewLimiter(3, 1), client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}

	var connected int
	if db.QueryRow("SELECT COUNT(*) FROM app_integrations WHERE provider='gmail'").Scan(&connected) == nil {
		service.gmailActive.Store(connected > 0)
	}
	var retryAt sql.NullString
	if db.QueryRow("SELECT retry_after_at FROM app_integrations WHERE provider='notion'").Scan(&retryAt) == nil && retryAt.Valid {
		if at, err := time.Parse(time.RFC3339Nano, retryAt.String); err == nil {
			service.notionRetryAt.Store(at.UnixNano())
		}
	}
	return service
}

// SetHTTPClient is for hermetic provider tests. Production always uses fixed API hosts.
func (s *Service) SetHTTPClient(c *http.Client) { s.client = c }

func (s *Service) Status(ctx context.Context, provider string) (Status, error) {
	out := Status{Provider: provider}
	var raw string
	err := s.db.QueryRowContext(ctx, "SELECT config,connected_at,last_success_at,last_error FROM app_integrations WHERE provider=?", provider).Scan(&raw, &out.ConnectedAt, &out.LastSuccess, &out.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal([]byte(raw), &out.Config); err != nil {
		return out, err
	}
	out.Connected = true
	return out, nil
}

func (s *Service) load(ctx context.Context, provider string) (string, string, Config, error) {
	var generation, encrypted, raw string
	var config Config
	err := s.db.QueryRowContext(ctx, "SELECT generation,credentials_enc,config FROM app_integrations WHERE provider=?", provider).Scan(&generation, &encrypted, &raw)
	if err != nil {
		return "", "", config, err
	}
	token, err := secret.Decrypt(s.key, encrypted)
	if err != nil {
		return "", "", config, errors.New("connector credentials could not be decrypted")
	}
	err = json.Unmarshal([]byte(raw), &config)
	return generation, token, config, err
}

func (s *Service) save(ctx context.Context, provider, userID, credential string, config Config) error {
	encrypted, err := secret.Encrypt(s.key, credential)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO app_integrations(provider,generation,credentials_enc,config,connected_by,connected_at)
 VALUES(?,?,?,?,?,?) ON CONFLICT(provider) DO UPDATE SET generation=excluded.generation,credentials_enc=excluded.credentials_enc,
 config=excluded.config,connected_by=excluded.connected_by,connected_at=excluded.connected_at,last_success_at=NULL,last_error=''`, provider, uid.New(), encrypted, string(raw), userID, time.Now().UTC().Format(time.RFC3339Nano))
	if err == nil && provider == "gmail" {
		s.gmailActive.Store(true)
	}
	return err
}

func (s *Service) Disconnect(ctx context.Context, provider string) error {
	if err := s.acquire(ctx, provider); err != nil {
		return err
	}
	defer s.release(provider)
	_, err := s.db.ExecContext(ctx, "DELETE FROM app_integrations WHERE provider=?", provider)
	if err == nil && provider == "gmail" {
		s.gmailActive.Store(false)
	}
	if err == nil && provider == "notion" {
		s.notionRetryAt.Store(0)
	}
	return err
}

func (s *Service) mark(ctx context.Context, provider, generation string, err error) {
	if err != nil {
		_, _ = s.db.ExecContext(ctx, "UPDATE app_integrations SET last_error=? WHERE provider=? AND generation=?", err.Error(), provider, generation)
		return
	}
	_, _ = s.db.ExecContext(ctx, "UPDATE app_integrations SET last_error='',last_success_at=? WHERE provider=? AND generation=?", time.Now().UTC().Format(time.RFC3339Nano), provider, generation)
}

func (s *Service) request(ctx context.Context, client *http.Client, method, url, token string, body any, out any, notion bool) error {
	if notion {
		if at := s.notionRetryAt.Load(); at > time.Now().UnixNano() {
			return providerRetry{at: time.Unix(0, at), attempted: false}
		}
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if notion {
		if err := s.notionRate.Wait(ctx); err != nil {
			return err
		}
		req.Header.Set("Notion-Version", "2025-09-03")
	}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("provider request failed; check the connection and try again")
	}
	defer res.Body.Close()
	if notion && (res.StatusCode == 429 || res.StatusCode == 529) {
		if seconds, err := strconv.ParseInt(res.Header.Get("Retry-After"), 10, 32); err == nil && seconds > 0 {
			at := time.Now().Add(time.Duration(seconds) * time.Second)
			s.notionRetryAt.Store(at.UnixNano())
			_, _ = s.db.ExecContext(ctx, "UPDATE app_integrations SET retry_after_at=? WHERE provider='notion'", at.UTC().Format(time.RFC3339Nano))
			return providerRetry{at: at, attempted: true}
		}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return providerHTTPError(res.StatusCode)
	}
	if out != nil {
		if err = json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(out); err != nil {
			return errInvalidResponse
		}
	}
	return nil
}

func (s *Service) EmailEnabled() bool {
	return s.gmailActive.Load() || s.fallback != nil && s.fallback.IsEnabled()
}

func (s *Service) acquire(ctx context.Context, provider string) error {
	gate := s.notionGate
	if provider == "gmail" {
		gate = s.gmailGate
	}
	select {
	case gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Service) release(provider string) {
	if provider == "gmail" {
		<-s.gmailGate
	} else {
		<-s.notionGate
	}
}

// The worker schedules this delay without blocking unrelated jobs or spending
// retry attempts on calls that have not reached the provider.
type providerRetry struct {
	at        time.Time
	attempted bool
}

func (e providerRetry) Error() string {
	return "Notion is rate limited; sync will retry after " + e.at.UTC().Format(time.RFC3339)
}
func (e providerRetry) RetryAt() time.Time     { return e.at }
func (e providerRetry) RequestAttempted() bool { return e.attempted }

var errInvalidResponse = errors.New("provider returned an invalid or oversized response")

type providerHTTPError int

func (e providerHTTPError) Error() string {
	return fmt.Sprintf("provider returned HTTP %d; check credentials and permissions", int(e))
}
