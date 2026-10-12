package appconnectors

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/calnode/calnode/internal/uid"
)

var notionIDPattern = regexp.MustCompile(`(?i)([0-9a-f]{8}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{12})$`)

func NotionID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil {
			return "", errors.New("invalid Notion page URL")
		}
		value = strings.TrimSuffix(u.Path, "/")
	}
	match := notionIDPattern.FindString(value)
	if match == "" {
		return "", errors.New("paste a Notion page URL or page ID")
	}
	raw := strings.ReplaceAll(match, "-", "")
	return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:], nil
}

func textParts(value string) []map[string]any {
	result := []map[string]any{}
	runes := []rune(value)
	for len(runes) > 0 {
		n := min(len(runes), 2000)
		result = append(result, map[string]any{"type": "text", "text": map[string]string{"content": string(runes[:n])}})
		runes = runes[n:]
	}
	return result
}

// ConnectNotion creates a dedicated database under a page explicitly chosen by the admin.
func (s *Service) ConnectNotion(ctx context.Context, userID, token, parent string) error {
	if err := s.acquire(ctx, "notion"); err != nil {
		return err
	}
	defer s.release("notion")
	id, err := NotionID(parent)
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("a Notion integration token is required")
	}
	status, err := s.Status(ctx, "notion")
	if err != nil {
		return err
	}
	if status.Connected {
		return errors.New("disconnect the existing Notion connection before choosing another destination")
	}
	var identity map[string]any
	if err = s.request(ctx, s.client, "GET", "https://api.notion.com/v1/users/me", token, nil, &identity, true); err != nil {
		return err
	}
	var page map[string]any
	if err = s.request(ctx, s.client, "GET", "https://api.notion.com/v1/pages/"+id, token, nil, &page, true); err != nil {
		return errors.New("Notion cannot access that page; share it with your integration")
	}
	properties := map[string]any{
		"Name":        map[string]any{"title": map[string]any{}},
		"OpenSlot ID": map[string]any{"rich_text": map[string]any{}},
		"Meeting":     map[string]any{"rich_text": map[string]any{}},
		"Guests":      map[string]any{"rich_text": map[string]any{}},
		"Time":        map[string]any{"date": map[string]any{}},
		"Status":      map[string]any{"select": map[string]any{"options": []map[string]string{{"name": "confirmed", "color": "blue"}, {"name": "cancelled", "color": "gray"}, {"name": "pending_payment", "color": "yellow"}}}},
	}
	var created struct {
		ID          string `json:"id"`
		URL         string `json:"url"`
		DataSources []struct {
			ID string `json:"id"`
		} `json:"data_sources"`
	}
	body := map[string]any{"parent": map[string]string{"type": "page_id", "page_id": id}, "title": textParts("OpenSlot bookings"), "initial_data_source": map[string]any{"properties": properties}}
	if err = s.request(ctx, s.client, "POST", "https://api.notion.com/v1/databases", token, body, &created, true); err != nil {
		return fmt.Errorf("database creation was not confirmed; inspect the parent page before retrying: %w", err)
	}
	if created.ID == "" || len(created.DataSources) != 1 || created.DataSources[0].ID == "" {
		return errors.New("Notion created a database but did not return its data source; inspect the selected page before retrying")
	}
	return s.save(ctx, "notion", userID, token, Config{Name: "OpenSlot bookings", DatabaseID: created.ID, DataSourceID: created.DataSources[0].ID, URL: created.URL})
}

func (s *Service) CheckNotion(ctx context.Context) error {
	if err := s.acquire(ctx, "notion"); err != nil {
		return err
	}
	defer s.release("notion")
	generation, token, config, err := s.load(ctx, "notion")
	if err != nil {
		return errors.New("connect Notion first")
	}
	var source struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	err = s.request(ctx, s.client, "GET", "https://api.notion.com/v1/data_sources/"+url.PathEscape(config.DataSourceID), token, nil, &source, true)
	if err == nil {
		for name, typ := range map[string]string{"Name": "title", "OpenSlot ID": "rich_text", "Meeting": "rich_text", "Guests": "rich_text", "Time": "date", "Status": "select"} {
			if source.Properties[name].Type != typ {
				err = fmt.Errorf("restore the Notion %s property (%s) before syncing", name, typ)
				break
			}
		}
	}
	s.mark(ctx, "notion", generation, err)
	return err
}

// QueueNotionBackfill is explicit; connecting only starts syncing future changes.
func (s *Service) QueueNotionBackfill(ctx context.Context) (int, error) {
	if err := s.acquire(ctx, "notion"); err != nil {
		return 0, err
	}
	defer s.release("notion")
	generation, _, _, err := s.load(ctx, "notion")
	if err != nil {
		return 0, errors.New("connect Notion first")
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM bookings")
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, id := range ids {
		body, _ := json.Marshal(map[string]string{"booking_id": id, "generation": generation, "event": uid.New()})
		if _, err = tx.ExecContext(ctx, "INSERT INTO jobs(id,type,payload,run_at) VALUES(?,?,?,?)", uid.New(), "notion.sync", string(body), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// SyncNotion reads current booking state, so delayed jobs cannot restore an older status.
func (s *Service) SyncNotion(ctx context.Context, payload string) error {
	// Finish below the worker's 30-second lease so another worker cannot claim an in-flight sync.
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if err := s.acquire(ctx, "notion"); err != nil {
		return err
	}
	defer s.release("notion")
	var event struct {
		BookingID  string `json:"booking_id"`
		Generation string `json:"generation"`
	}
	if json.Unmarshal([]byte(payload), &event) != nil || event.BookingID == "" || event.Generation == "" {
		return errors.New("invalid Notion sync job")
	}
	generation, token, config, err := s.load(ctx, "notion")
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if generation != event.Generation {
		return nil
	}
	err = s.syncNotionBooking(ctx, token, config, generation, event.BookingID)
	s.mark(ctx, "notion", generation, err)
	return err
}

func (s *Service) syncNotionBooking(ctx context.Context, token string, config Config, generation, bookingID string) error {
	var name, start, end, status, payment string
	err := s.db.QueryRowContext(ctx, `SELECT e.name,b.start_at,b.end_at,b.status,b.payment_status FROM bookings b JOIN event_types e ON b.event_type_id=e.id WHERE b.id=?`, bookingID).Scan(&name, &start, &end, &status, &payment)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT name,email FROM booking_attendees WHERE booking_id=? ORDER BY id", bookingID)
	if err != nil {
		return err
	}
	guests := []string{}
	for rows.Next() {
		var n, e string
		if err = rows.Scan(&n, &e); err != nil {
			rows.Close()
			return err
		}
		guests = append(guests, n+" <"+e+">")
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// SQLite stores UTC with a space; Notion requires ISO 8601.
	normalize := func(value string) string {
		if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return t.Format(time.RFC3339)
		}
		if t, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
			return t.Format(time.RFC3339)
		}
		return value
	}
	if status == "confirmed" && payment == "pending" {
		status = "pending_payment"
	}
	properties := map[string]any{
		"Name": map[string]any{"title": textParts(name)}, "OpenSlot ID": map[string]any{"rich_text": textParts(bookingID)},
		"Meeting": map[string]any{"rich_text": textParts(name)}, "Guests": map[string]any{"rich_text": textParts(strings.Join(guests, ", "))},
		"Time": map[string]any{"date": map[string]string{"start": normalize(start), "end": normalize(end)}}, "Status": map[string]any{"select": map[string]string{"name": status}},
	}
	var pageID string
	err = s.db.QueryRowContext(ctx, "SELECT page_id FROM notion_booking_pages WHERE generation=? AND booking_id=?", generation, bookingID).Scan(&pageID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if pageID == "" {
		// Recover a remote success whose local receipt was lost before retrying creation.
		var found struct {
			Results []struct {
				ID string `json:"id"`
			} `json:"results"`
		}
		query := map[string]any{"filter": map[string]any{"property": "OpenSlot ID", "rich_text": map[string]string{"equals": bookingID}}}
		if err = s.request(ctx, s.client, "POST", "https://api.notion.com/v1/data_sources/"+url.PathEscape(config.DataSourceID)+"/query", token, query, &found, true); err != nil {
			return err
		}
		if len(found.Results) > 0 {
			pageID = found.Results[0].ID
		}
	}
	var page struct {
		ID string `json:"id"`
	}
	if pageID == "" {
		err = s.request(ctx, s.client, "POST", "https://api.notion.com/v1/pages", token, map[string]any{"parent": map[string]string{"type": "data_source_id", "data_source_id": config.DataSourceID}, "properties": properties}, &page, true)
		if err != nil {
			return err
		}
		pageID = page.ID
		if pageID == "" {
			return errors.New("Notion did not return a page ID")
		}
	} else if err = s.request(ctx, s.client, "PATCH", "https://api.notion.com/v1/pages/"+url.PathEscape(pageID), token, map[string]any{"properties": properties}, &page, true); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO notion_booking_pages(generation,booking_id,page_id) VALUES(?,?,?) ON CONFLICT(generation,booking_id) DO UPDATE SET page_id=excluded.page_id", generation, bookingID, pageID)
	return err
}
